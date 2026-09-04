import type { ThreadMessageLike } from '@assistant-ui/react'

import type { RunTimeline, TimelineEntry } from '@/lib/runs/timeline'

export type RenderEntry = Extract<TimelineEntry, { kind: 'render' }>
export type GateEntry = Extract<TimelineEntry, { kind: 'gate' }>

/**
 * 一轮问答。一段对话由多轮组成，每一轮对应后端的一次运行。
 *
 * 跑完的轮次把产出冻结在 `answer` 里，正在跑的那一轮不冻结、每帧从实时
 * timeline 现算——两者在下面 turnsToMessages 里合成同一个消息数组。
 */
export interface ChatTurn {
  /** 稳定 key，消息 id 由它派生。 */
  id: string
  runId: string
  question: string
  answer?: FrozenAnswer
}

export interface FrozenAnswer {
  text: string
  /** 各节点的思维链拼在一起——只有资源作者能看到（node.reasoning 是
   * IsInternal 事件，黑盒订阅者的事件流里本来就没有这些事件）。 */
  reasoningText: string
  entries: TimelineEntry[]
  failed: boolean
  /** 失败原因，或"连接中断"这类没有模型输出时的说明。 */
  note?: string
}

/** 把一次运行的 timeline 冻结成一轮的产出。 */
export function freezeAnswer(timeline: RunTimeline, note?: string): FrozenAnswer {
  return {
    text: joinBubbleText(timeline),
    reasoningText: joinReasoningText(timeline),
    entries: timeline.entries,
    failed: timeline.runStatus === 'failed',
    note: note ?? timeline.runError,
  }
}

/**
 * 各节点的输出拼成一条回答。
 *
 * 单 Agent 的运行只有一个节点，拼出来就是它自己；多节点的 Bundle 按
 * timeline 的顺序接起来。节点名不在这里加——加了每条消息都会带一堆
 * "writer:" 前缀，真正需要分节点看的是运行详情页的执行图。
 */
function joinBubbleText(timeline: RunTimeline): string {
  return Object.values(timeline.bubbles)
    .map((b) => b.text)
    .filter(Boolean)
    .join('\n')
}

function joinReasoningText(timeline: RunTimeline): string {
  return Object.values(timeline.bubbles)
    .map((b) => b.reasoningText)
    .filter(Boolean)
    .join('\n')
}

/**
 * 把若干轮问答摊成 assistant-ui 的消息数组。
 *
 * 每一轮出两条消息：用户那条是原话，助手那条按 timeline 拆成若干
 * part——文本、插件渲染卡、人工审批卡。后两者走 `data-` 前缀的自定义
 * part，在 AgentThread 里由 Fallback 渲染器按 type 分派。
 */
export function turnsToMessages(
  turns: ChatTurn[],
  live: { runId?: string; timeline: RunTimeline } | undefined,
): ThreadMessageLike[] {
  const out: ThreadMessageLike[] = []
  for (const turn of turns) {
    out.push({ role: 'user', id: `${turn.id}:u`, content: turn.question })

    const isLive = turn.answer === undefined && live?.runId === turn.runId
    const answer = turn.answer ?? (isLive && live ? freezeAnswer(live.timeline) : undefined)
    out.push({
      role: 'assistant',
      id: `${turn.id}:a`,
      status: isLive
        ? { type: 'running' }
        : answer?.failed
          ? { type: 'incomplete', reason: 'error' }
          : { type: 'complete', reason: 'stop' },
      content: answerParts(answer, isLive),
    })
  }
  return out
}

/** ThreadMessageLike.content 里的一个 part。 */
type Part = Exclude<ThreadMessageLike['content'], string>[number]

function answerParts(answer: FrozenAnswer | undefined, isLive: boolean): Part[] {
  const parts: Part[] = []
  const reasoningText = answer?.reasoningText ?? ''
  const text = answer?.text ?? ''
  // 思维链要排在正文前面——assistant-ui 是按"先想后答"的顺序渲染折叠块
  // 的，放反了看起来会像模型读完答案才去思考。status 驱动折叠块的自动
  // 展开/收起：还在跑而且答案正文一个字都没出来，说明模型还纯在思考，
  // 这时候是 running（前端据此自动展开）；一旦开始吐正文或者这一轮已经
  // 结束，就是 complete（自动收起成一行摘要）。
  if (reasoningText) {
    parts.push({
      type: 'reasoning',
      text: reasoningText,
      status: isLive && !text ? { type: 'running' } : { type: 'complete' },
    })
  }
  // 正在跑但还没吐字时也要占一个空文本 part：一条 part 都没有的助手消息
  // 在 assistant-ui 里什么都不画，用户会以为发送没生效。
  if (text || isLive) parts.push({ type: 'text', text })
  if (!text && !isLive && answer?.note) parts.push({ type: 'text', text: answer.note })

  for (const entry of answer?.entries ?? []) {
    if (entry.kind === 'render') {
      parts.push({ type: 'data-plugin-render', data: entry })
    } else if (entry.kind === 'gate') {
      parts.push({ type: 'data-gate', data: entry })
    }
  }
  // 有正文又失败了：正文留着，失败原因另起一块，不要把两者拼成一段。
  if (text && answer?.failed && answer.note) {
    parts.push({ type: 'data-run-error', data: { note: answer.note } })
  }
  return parts
}
