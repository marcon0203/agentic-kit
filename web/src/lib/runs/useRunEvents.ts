import { useEffect, useRef, useState } from 'react'

import type { components } from '@/lib/api/schema'
import { useAuthStore } from '@/lib/auth/store'

export type RunEvent = components['schemas']['RunEvent']

export type StreamStatus = 'connecting' | 'open' | 'reconnecting' | 'closed' | 'error'

interface UseRunEventsResult {
  events: RunEvent[]
  status: StreamStatus
  reconnect: () => void
}

/**
 * Consumes GET /runs/{id}/stream (spec-12's chunked NDJSON) and accumulates
 * every event it has seen. Reconnects with `after_id` on a dropped
 * connection so history is neither duplicated nor lost (spec-14's
 * acceptance checklist).
 *
 * The StrictMode double-mount guard below is the exact fix spec-14 calls
 * out from the POC: React 18/19 dev mode mounts effects twice, so the
 * first instance's fetch gets aborted — but its `finally` still runs and
 * would otherwise stomp the second instance's state with `status:
 * "closed"` a moment after it correctly set `status: "open"`. Guarding
 * every state update behind the `cancelled` closure variable (not just a
 * ref check on unmount) is what spec-14 explicitly requires.
 */
export function useRunEvents(runId: string | undefined): UseRunEventsResult {
  const [events, setEvents] = useState<RunEvent[]>([])
  const [status, setStatus] = useState<StreamStatus>('connecting')
  const [reconnectNonce, setReconnectNonce] = useState(0)
  const lastIdRef = useRef(0)

  // 换一次运行就把累积状态清干净。不清的话上一次的 bundle.finished 还留在
  // events 里，下一次运行刚开始就被判成"已完成"——试运行面板第二条消息发
  // 出去就秒回上一条的答案，压根没跑；lastIdRef 也一样，带着上一轮的游标去
  // 请求下一轮的流是没道理的。
  //
  // 在渲染期直接改而不放进 effect：effect 要等这一次渲染提交完才跑，中间
  // 那一帧消费方仍会拿旧 events 算出旧状态，误判就发生在那一帧。渲染期
  // setState 会让 React 丢掉这次渲染结果重来，旧值不会外泄——这正是官方
  // 「props 变了顺手调整 state」的用法。
  const seenRunRef = useRef<string | undefined>(undefined)
  if (seenRunRef.current !== runId) {
    seenRunRef.current = runId
    lastIdRef.current = 0
    if (events.length > 0) setEvents([])
    // status 同理：上一次运行断在 'error' 上，下一次运行刚挂上来就会被消费
    // 方当成"这次也断了"。没有 runId 时归到 'closed'——那是"没有在看任何
    // 运行"，不是"连接出问题了"。
    const fresh: StreamStatus = runId ? 'connecting' : 'closed'
    if (status !== fresh) setStatus(fresh)
  }

  useEffect(() => {
    if (!runId) return
    const controller = new AbortController()
    let cancelled = false

    async function run() {
      setStatus((s) => (s === 'error' ? 'reconnecting' : 'connecting'))
      try {
        const token = useAuthStore.getState().accessToken
        const url = `/api/v1/runs/${runId}/stream${lastIdRef.current > 0 ? `?after_id=${lastIdRef.current}` : ''}`
        const res = await fetch(url, {
          signal: controller.signal,
          headers: token ? { Authorization: `Bearer ${token}` } : undefined,
        })
        if (!res.ok || !res.body) {
          if (!cancelled) setStatus('error')
          return
        }
        if (!cancelled) setStatus('open')

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        let sawTerminalEvent = false
        let sawStreamError = false
        while (true) {
          const { value, done } = await reader.read()
          if (done || cancelled) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? '' // last chunk may be a partial line

          for (const line of lines) {
            if (!line.trim()) continue
            let parsed: RunEvent
            try {
              parsed = JSON.parse(line) as RunEvent
            } catch {
              continue
            }
            if (parsed.type === 'stream.error') {
              sawStreamError = true
              continue
            }
            lastIdRef.current = parsed.id
            if (!cancelled) {
              setEvents((prev) => [...prev, parsed])
            }
            if (parsed.type === 'bundle.finished' || parsed.type === 'bundle.failed') {
              sawTerminalEvent = true
            }
          }
        }
        // The connection ending without a terminal bundle event means it
        // dropped unexpectedly (network blip, proxy timeout, or a
        // stream.error the server pushed before closing) — surface it as
        // reconnectable, not "done".
        if (!cancelled) {
          setStatus(sawTerminalEvent && !sawStreamError ? 'closed' : 'error')
        }
      } catch (err) {
        if (!cancelled && !(err instanceof DOMException && err.name === 'AbortError')) {
          setStatus('error')
        }
      }
    }

    run()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [runId, reconnectNonce])

  return {
    events,
    status,
    reconnect: () => setReconnectNonce((n) => n + 1),
  }
}
