import { buildTimeline } from '@/lib/runs/timeline'
import type { RunEvent } from '@/lib/runs/useRunEvents'
import { freezeAnswer, type ChatTurn } from '@/lib/runs/threadMessages'
import { useAuthStore } from '@/lib/auth/store'

/**
 * 把一次**已经结束**的运行的事件流一次性读完。
 *
 * /runs/{id}/stream 对终态的运行会把全部事件补发一遍然后立刻关闭——它本
 * 来就是断线重连用的补发通道，重建历史正好复用，不必再造一个"取运行全部
 * 事件"的接口。
 */
export async function fetchRunEvents(runID: string, signal?: AbortSignal): Promise<RunEvent[]> {
  const token = useAuthStore.getState().accessToken
  const res = await fetch(`/api/v1/runs/${runID}/stream`, {
    signal,
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (!res.ok || !res.body) throw new Error(`读取运行 ${runID} 的事件流失败`)

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  const out: RunEvent[] = []
  let buffer = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      if (!line.trim()) continue
      try {
        const ev = JSON.parse(line) as RunEvent
        if (ev.type !== 'stream.error') out.push(ev)
      } catch {
        // 半行或坏行，跳过——补发通道里这不是致命错误。
      }
    }
  }
  return out
}

/**
 * 重建一段对话里已经结束的那些轮次。
 *
 * `skipRunID` 是当前正在实时跟随的那次运行，交给 useRunEvents 去流，不
 * 在这里冻结——否则同一轮会出现两次。
 */
export async function replayFinishedTurns(
  runIDs: string[],
  skipRunID: string | undefined,
  signal?: AbortSignal,
): Promise<ChatTurn[]> {
  const turns: ChatTurn[] = []
  for (const runID of runIDs) {
    if (runID === skipRunID) continue
    const events = await fetchRunEvents(runID, signal)
    const timeline = buildTimeline(events)
    turns.push({
      id: `run:${runID}`,
      runId: runID,
      // 拿不到原话时留一句占位，而不是空白气泡——空白让人以为消息丢了。
      question: timeline.userMessage ?? '（这次运行没有记录输入）',
      answer: freezeAnswer(timeline),
    })
  }
  return turns
}
