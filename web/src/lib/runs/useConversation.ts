import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { ApiError } from '@/lib/api/client'
import { buildTimeline } from '@/lib/runs/timeline'
import { useRunEvents } from '@/lib/runs/useRunEvents'
import { freezeAnswer, turnsToMessages, type ChatTurn } from '@/lib/runs/threadMessages'

/** 调用方负责发起一次运行——试运行面板发 /runs/agent-test，应用运行页发 /runs。 */
export type StartRun = (question: string, sessionID: string | undefined) => Promise<{ run_id: string; session_id?: string }>

export interface ConversationOptions {
  start: StartRun
  /** 接着已有的一段对话继续（页面刷新后从后端读回来的）。 */
  initialSessionID?: string
  /** 页面刷新后重建出来的历史轮次。 */
  initialTurns?: ChatTurn[]
  /**
   * 打开页面时**已经在跑**的那次运行，直接接上去实时跟随。
   *
   * 应用运行页是从一个 run_id 进来的，那次运行不是这个 hook 发起的，不
   * 交代一声它就没人跟——界面会一直停在"还没开始"。
   */
  initialActiveRunID?: string
  /** 配置不完整之类的原因，暂时不让发。 */
  blocked?: boolean
  /** 见 useRunEvents 同名参数——匿名"立即体验"页用自己的访客 token 源
   * 顶替平台的 useAuthStore，两套鉴权体系不能互相感知。 */
  getAccessToken?: () => string | null
}

/**
 * 一段对话的状态机：发消息、跟实时事件流、把结果冻结成一轮，供
 * assistant-ui 的 useExternalStoreRuntime 消费。
 *
 * 这里刻意不把传输交给 assistant-ui：数据源仍是现成的 NDJSON 事件流加
 * buildTimeline，assistant-ui 只负责渲染和交互。人工审批、插件渲染卡这些
 * 东西 timeline 里已经有了，换个 UI 库不该把它们重做一遍。
 */
export function useConversation({
  start,
  initialSessionID,
  initialTurns,
  initialActiveRunID,
  blocked,
  getAccessToken,
}: ConversationOptions) {
  const [sessionID, setSessionID] = useState<string | undefined>(initialSessionID)
  const [turns, setTurns] = useState<ChatTurn[]>(initialTurns ?? [])
  const [activeRunID, setActiveRunID] = useState<string | undefined>(initialActiveRunID)
  const [error, setError] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const seq = useRef(0)

  // 外部（刷新后的历史）到得比这个 hook 晚时补上。只在还没开始对话时接管，
  // 免得把用户已经发出去的消息冲掉。
  useEffect(() => {
    if (initialSessionID) setSessionID((s) => s ?? initialSessionID)
  }, [initialSessionID])
  useEffect(() => {
    if (initialTurns && initialTurns.length > 0) {
      setTurns((cur) => (cur.length === 0 ? initialTurns : cur))
    }
  }, [initialTurns])

  // 接管一次外部已经在跑的运行。它还没有对应的轮次，先补一条问题待定的
  // ——原话在事件流的 bundle.started 里，下面拿到就补上。
  useEffect(() => {
    if (!initialActiveRunID) return
    setTurns((cur) =>
      cur.some((t) => t.runId === initialActiveRunID)
        ? cur
        : [...cur, { id: `run:${initialActiveRunID}`, runId: initialActiveRunID, question: '' }],
    )
    setActiveRunID((cur) => cur ?? initialActiveRunID)
  }, [initialActiveRunID])

  const { events, status: streamStatus, reconnect } = useRunEvents(activeRunID, getAccessToken)
  const timeline = useMemo(() => buildTimeline(events), [events])

  // 接管来的那一轮，问题原话要等事件流里的 bundle.started 才知道。
  useEffect(() => {
    const question = timeline.userMessage
    if (!activeRunID || !question) return
    setTurns((cur) => cur.map((t) => (t.runId === activeRunID && !t.question ? { ...t, question } : t)))
  }, [timeline.userMessage, activeRunID])

  const isRunning = starting || activeRunID !== undefined

  /** 收尾当前这一轮：把产出冻结进 turns，放开输入。 */
  const settle = useCallback((runID: string, note?: string) => {
    setTurns((cur) =>
      cur.map((t) => (t.runId === runID && t.answer === undefined ? { ...t, answer: freezeAnswer(timeline, note) } : t)),
    )
    setActiveRunID(undefined)
  }, [timeline])

  // 正常收尾：拿到 bundle.finished / bundle.failed。
  useEffect(() => {
    if (!activeRunID) return
    if (timeline.runStatus !== 'finished' && timeline.runStatus !== 'failed') return
    settle(activeRunID)
  }, [timeline.runStatus, activeRunID, settle])

  // 兜底：流断在终态事件之前。后端那条竞态已经修掉了，但网络抖动、代理超
  // 时这些外部原因还是会断流；不兜底的话 activeRunID 一直挂着，输入框跟着
  // 永远禁用——"发了一次之后再也发不出去"就是这么来的。
  useEffect(() => {
    if (!activeRunID || streamStatus !== 'error') return
    setError('运行事件流中断了')
    settle(activeRunID, '连接中断，没有收到运行结果')
  }, [streamStatus, activeRunID, settle])

  const send = useCallback(
    async (question: string) => {
      const text = question.trim()
      if (!text || blocked || isRunning) return
      setError(null)
      setStarting(true)
      const turnID = `turn-${(seq.current += 1)}`
      try {
        const created = await start(text, sessionID)
        if (created.session_id) setSessionID(created.session_id)
        setTurns((cur) => [...cur, { id: turnID, runId: created.run_id, question: text }])
        setActiveRunID(created.run_id)
      } catch (err) {
        setError(err instanceof ApiError ? err.message : '没能启动运行，请稍后重试')
        throw err
      } finally {
        setStarting(false)
      }
    },
    [start, sessionID, blocked, isRunning],
  )

  const messages = useMemo(
    () => turnsToMessages(turns, { runId: activeRunID, timeline }),
    [turns, activeRunID, timeline],
  )

  return {
    events,
    messages,
    turns,
    isRunning,
    send,
    error,
    clearError: useCallback(() => setError(null), []),
    sessionID,
    activeRunID,
    streamStatus,
    reconnect,
    timeline,
  }
}
