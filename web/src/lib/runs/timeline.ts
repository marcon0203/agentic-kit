import type { RunEvent } from '@/lib/runs/useRunEvents'

export interface ToolCallState {
  name: string
  status: 'started' | 'finished'
}

export interface NodeBubbleState {
  node: string
  text: string
  toolCalls: ToolCallState[]
  status: 'running' | 'done' | 'failed'
  /** true while still receiving node.thinking chunks — drives aria-busy
   * and the typewriter cursor; spec-14's a11y note: only announce the
   * full bubble via aria-live once this flips to false. */
  isBusy: boolean
  errorText?: string
}

export type TimelineEntry =
  | { kind: 'bubble-group'; key: string; nodes: string[] }
  | { kind: 'gate'; key: string; node: string; gateId: number | null; status: 'pending' | 'approved' | 'rejected' | 'timeout' }
  | { kind: 'system'; key: string; text: string; tone: 'info' | 'error' | 'success' }

export interface RunTimeline {
  entries: TimelineEntry[]
  bubbles: Record<string, NodeBubbleState>
  runStatus: 'idle' | 'running' | 'finished' | 'failed'
  runError?: string
}

function payloadOf(ev: RunEvent): Record<string, unknown> {
  return (ev.payload as Record<string, unknown>) ?? {}
}

/**
 * Reduces the flat event log into chat bubbles + inline gate cards +
 * system status lines, grouping nodes that are running at the same time
 * into one bubble-group entry (spec-14's parallel-Agent layout — the
 * criterion is "two node.started-equivalents without a node.finished
 * between them").
 */
export function buildTimeline(events: RunEvent[]): RunTimeline {
  const bubbles: Record<string, NodeBubbleState> = {}
  const entries: TimelineEntry[] = []
  let openGroup: Extract<TimelineEntry, { kind: 'bubble-group' }> | null = null
  let runStatus: RunTimeline['runStatus'] = 'idle'
  let runError: string | undefined

  function bubbleFor(node: string): NodeBubbleState {
    if (!bubbles[node]) {
      bubbles[node] = { node, text: '', toolCalls: [], status: 'running', isBusy: true }
    }
    return bubbles[node]
  }

  function groupHasRunningMember(group: TimelineEntry & { kind: 'bubble-group' }): boolean {
    return group.nodes.some((n) => bubbles[n]?.status === 'running')
  }

  function openOrAttachGroup(node: string) {
    if (bubbles[node]) {
      // Already part of some group — nothing to attach, just update state.
      return
    }
    if (openGroup && groupHasRunningMember(openGroup)) {
      openGroup.nodes.push(node)
    } else {
      openGroup = { kind: 'bubble-group', key: `group-${entries.length}-${node}`, nodes: [node] }
      entries.push(openGroup)
    }
    bubbleFor(node)
  }

  function closeGroup() {
    openGroup = null
  }

  for (const ev of events) {
    const node = ev.node ?? ''
    const payload = payloadOf(ev)

    switch (ev.type) {
      case 'bundle.started':
        runStatus = 'running'
        break
      case 'bundle.finished':
        runStatus = 'finished'
        closeGroup()
        entries.push({ kind: 'system', key: `sys-${ev.id}`, text: '运行已完成', tone: 'success' })
        break
      case 'bundle.failed':
        runStatus = 'failed'
        runError = typeof payload.error === 'string' ? payload.error : undefined
        closeGroup()
        entries.push({
          kind: 'system',
          key: `sys-${ev.id}`,
          text: runError ? `运行失败：${runError}` : '运行失败',
          tone: 'error',
        })
        break

      case 'node.started':
      case 'node.queued':
        openOrAttachGroup(node)
        break

      case 'node.thinking': {
        openOrAttachGroup(node)
        const b = bubbleFor(node)
        const text = typeof payload.text === 'string' ? payload.text : ''
        b.text += text
        b.isBusy = true
        break
      }

      case 'node.tool_call.started': {
        openOrAttachGroup(node)
        const b = bubbleFor(node)
        const name = typeof payload.name === 'string' ? payload.name : '工具'
        b.toolCalls = [...b.toolCalls, { name, status: 'started' }]
        break
      }

      case 'node.tool_call.finished': {
        openOrAttachGroup(node)
        const b = bubbleFor(node)
        const name = typeof payload.name === 'string' ? payload.name : '工具'
        const idx = [...b.toolCalls].reverse().findIndex((t) => t.name === name && t.status === 'started')
        if (idx >= 0) {
          const realIdx = b.toolCalls.length - 1 - idx
          b.toolCalls = b.toolCalls.map((t, i) => (i === realIdx ? { ...t, status: 'finished' } : t))
        }
        break
      }

      case 'node.finished': {
        openOrAttachGroup(node)
        const b = bubbleFor(node)
        const text = typeof payload.text === 'string' ? payload.text : undefined
        if (text) b.text = text
        b.status = 'done'
        b.isBusy = false
        break
      }

      case 'node.failed': {
        openOrAttachGroup(node)
        const b = bubbleFor(node)
        b.status = 'failed'
        b.isBusy = false
        b.errorText = typeof payload.error === 'string' ? payload.error : '节点执行失败'
        break
      }

      case 'human_gate.waiting': {
        closeGroup()
        const gateId = typeof payload.gate_id === 'number' ? payload.gate_id : null
        entries.push({ kind: 'gate', key: `gate-${ev.id}`, node, gateId, status: 'pending' })
        break
      }

      case 'human_gate.resolved': {
        const status = typeof payload.status === 'string' ? payload.status : 'approved'
        const normalized: Extract<TimelineEntry, { kind: 'gate' }>['status'] =
          status === 'rejected' ? 'rejected' : status === 'timeout' ? 'timeout' : 'approved'
        for (let i = entries.length - 1; i >= 0; i--) {
          const e = entries[i]
          if (e.kind === 'gate' && e.node === node && e.status === 'pending') {
            entries[i] = { ...e, status: normalized }
            break
          }
        }
        break
      }

      default:
        break
    }
  }

  return { entries, bubbles, runStatus, runError }
}
