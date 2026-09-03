import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { useAuthStore } from '@/lib/auth/store'
import { AgentThread } from '@/components/chat/AgentThread'
import { useConversation, type StartRun } from '@/lib/runs/useConversation'
import { replayFinishedTurns } from '@/lib/runs/replaySession'
import type { ChatTurn } from '@/lib/runs/threadMessages'
import { RunHeader } from '@/components/run/RunHeader'
import { RunSidebar } from '@/components/run/RunSidebar'
import type { PlatformStatus } from '@/components/run/StatusChip'

type RunDetail = components['schemas']['RunDetail']
type RunSummary = components['schemas']['RunSummary']

/**
 * 应用运行页。
 *
 * 这一页原来是个一次性的"运行查看器"：没有输入框，看完这一次就完了，想
 * 再问一句只能回去重新发起。现在它是一段**对话**——同一个 session 里连着
 * 发消息，模型接得住上文；刷新页面也能把整段对话重建出来。
 *
 * 界面换成了 assistant-ui（AgentThread），右边的执行图/共享状态侧栏保持
 * 原样：那一栏是"这次运行内部发生了什么"，和对话本身是两件事。
 */
export function RunPage() {
  const { runId } = useParams<{ runId: string }>()
  const navigate = useNavigate()
  const userId = useAuthStore((s) => s.user?.id)
  const queryClient = useQueryClient()

  const runQuery = useQuery({
    queryKey: ['run', runId],
    queryFn: async () => unwrap<RunDetail>(await apiClient.GET('/runs/{id}', { params: { path: { id: runId! } } })),
    enabled: !!runId,
  })
  const run = runQuery.data
  const sessionID = run?.session_id

  // 这段对话里的全部运行，用来重建历史。老运行没有 session_id，那就只有
  // 眼前这一次——如实呈现，不硬凑成一段对话。
  const sessionQuery = useQuery({
    queryKey: ['session-runs', sessionID],
    queryFn: async () =>
      unwrap<RunSummary[]>(await apiClient.GET('/sessions/{id}/runs', { params: { path: { id: sessionID! } } })),
    enabled: !!sessionID,
  })

  // 地址栏里那次运行还在跑，就交给 useConversation 实时跟；已经结束了，
  // 它和其它历史轮次一样由重放补回来。
  const liveRunID = run?.status === 'running' ? runId : undefined

  const [history, setHistory] = useState<ChatTurn[] | undefined>(undefined)
  useEffect(() => {
    const rows = sessionQuery.data
    if (!run) return
    // 没有 session_id 的老运行不属于任何一段对话，就只有眼前这一次。
    const ids = rows ? rows.map((r) => r.run_id) : runId ? [runId] : []
    const controller = new AbortController()
    replayFinishedTurns(ids, liveRunID, controller.signal)
      .then(setHistory)
      .catch(() => {
        // 重建失败不该把整页拖垮：至少还能接着往下聊。
        setHistory([])
      })
    return () => controller.abort()
  }, [sessionQuery.data, run, runId, liveRunID])

  const start = useCallback<StartRun>(
    async (question, sid) => {
      const created = unwrap<RunSummary>(
        await apiClient.POST('/runs', {
          body: {
            bundle_ref: run?.bundle_ref ?? '',
            ...(run?.bundle_version ? { bundle_version: run.bundle_version } : {}),
            input: { message: question },
            ...(sid ? { session_id: sid } : {}),
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      // 地址栏跟着走到最新那次运行：刷新、分享链接拿到的都是这段对话的当
      // 前位置，而不是它开头的那一次。
      navigate(`/runs/${created.run_id}`, { replace: true })
      return created
    },
    [run?.bundle_ref, run?.bundle_version, navigate],
  )

  const chat = useConversation({
    start,
    initialSessionID: sessionID,
    initialTurns: history,
    // 历史还没重建完就别接管实时那一轮，否则它会排在历史前面。
    initialActiveRunID: history !== undefined ? liveRunID : undefined,
    blocked: !run?.bundle_ref,
  })

  // 一轮跑完后回头刷运行详情：用量、共享状态这些只在运行行上，事件流里
  // 没有。
  const settledRun = chat.activeRunID === undefined
  useEffect(() => {
    if (settledRun && runId) queryClient.invalidateQueries({ queryKey: ['run', runId] })
  }, [settledRun, runId, queryClient])

  const [actionError, setActionError] = useState<string | null>(null)

  const resolveGate = useCallback(
    async (node: string, approved: boolean) => {
      const target = chat.activeRunID ?? runId
      if (!target) return
      setActionError(null)
      try {
        assertOk(await apiClient.POST('/runs/{id}/gate', { params: { path: { id: target } }, body: { node, approved } }))
      } catch (err) {
        setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试')
        throw err
      }
    },
    [chat.activeRunID, runId],
  )

  const stopRun = useCallback(async () => {
    const target = chat.activeRunID ?? runId
    if (!target) return
    setActionError(null)
    try {
      assertOk(await apiClient.POST('/runs/{id}/cancel', { params: { path: { id: target } } }))
      queryClient.invalidateQueries({ queryKey: ['run', target] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '停止失败，请稍后重试')
      throw err
    }
  }, [chat.activeRunID, runId, queryClient])

  const isBlackbox = run ? !run.is_owner : false
  // V1's only valid approver is the run's own triggered_by user (spec-11)
  // — the frontend has no other way to know that than "I created this
  // run's session", which for the owner/subscriber both means: I'm the
  // one who is signed in and looking at a run of my own bundle_ref that
  // I triggered. Since RunSummary doesn't expose triggered_by, treat
  // "is_owner" as the proxy: only a run's own initiator can be its
  // approver in V1, and that's always the Bundle owner running their own
  // Bundle directly (a subscriber never gates on someone else's bundle
  // by this rule) — a real per-run triggered_by check happens
  // server-side (403 + 50004) regardless of what this button shows.
  const canApprove = !!userId && !!run?.is_owner

  const runStatus: PlatformStatus =
    run?.status === 'failed'
      ? 'failed'
      : run?.status === 'finished'
        ? 'done'
        : chat.timeline.runStatus === 'failed'
          ? 'failed'
          : 'running'

  const gate = useMemo(() => ({ canApprove, onResolve: resolveGate }), [canApprove, resolveGate])

  if (!runId) return null

  return (
    <div className="grid grid-cols-1 gap-space-6 lg:grid-cols-[1.08fr_.92fr]">
      <div className="flex min-h-0 flex-col">
        <RunHeader
          runId={chat.activeRunID ?? runId}
          status={runStatus}
          streamStatus={chat.streamStatus}
          totalTokens={run?.usage?.total_tokens ?? 0}
          costUsd={run?.usage?.cost_usd ?? 0}
          onReconnect={chat.reconnect}
          onStop={stopRun}
        />

        {actionError && (
          <p role="alert" className="text-body-sm mb-space-4 text-rust">
            {actionError}
          </p>
        )}

        <div className="flex max-h-[72vh] min-h-[24rem] flex-1 flex-col overflow-hidden rounded-lg border border-border bg-surface">
          <AgentThread
            messages={chat.messages}
            isRunning={chat.isRunning}
            onSend={chat.send}
            onCancel={stopRun}
            gate={gate}
            disabled={!run?.bundle_ref}
            disabledHint={run ? undefined : '正在读取这次运行…'}
            emptyTitle={run?.bundle_ref ?? '运行'}
            emptyHint="接着往下问，模型看得到上文"
            footerNote={
              chat.error ? (
                <p role="alert" className="text-caption text-rust">
                  {chat.error}
                </p>
              ) : undefined
            }
          />
        </div>
      </div>

      <RunSidebar
        bubbles={chat.timeline.bubbles}
        events={chat.events}
        sharedState={(run?.shared_state as Record<string, unknown>) ?? {}}
        isBlackbox={isBlackbox}
        totalTokens={run?.usage?.total_tokens ?? 0}
        costUsd={run?.usage?.cost_usd ?? 0}
        durationSeconds={run?.usage?.duration_seconds ?? 0}
      />
    </div>
  )
}
