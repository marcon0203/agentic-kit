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
  /** spec-20 §4.2's node.render event — a plugin renderer took over this
   * node's output (or an explicit tool call handed it a result to render).
   * resourceUri is what the iframe card fetches its content from, via the
   * independent /plugins/assets/{plugin}/{version}/* asset domain. */
  | {
      kind: 'render'
      key: string
      node: string
      plugin: string
      version: string
      renderer: string
      resourceUri: string
      entry: string
      data: unknown
    }

export interface RunTimeline {
  entries: TimelineEntry[]
  bubbles: Record<string, NodeBubbleState>
  runStatus: 'idle' | 'running' | 'finished' | 'failed'
  runError?: string
}

function payloadOf(ev: RunEvent): Record<string, unknown> {
  return (ev.payload as Record<string, unknown>) ?? {}
}

const EMPTY_LANG_SET: ReadonlySet<string> = new Set()

/**
 * Strips fenced code blocks whose language is in hiddenLangs out of raw —
 * a renderer-eligible block (e.g. ```chart) is about to get its own render
 * card, so its raw JSON has no business flashing on screen as chat text
 * first. An ordinary fenced block the model wrote for its own reasons
 * (```python, say) passes through untouched, markers and all.
 *
 * Runs on the *whole* accumulated text on every call rather than parsing
 * incrementally chunk-by-chunk — chat messages are short enough that
 * re-scanning from scratch each delta is cheap, and it sidesteps every
 * edge case a streaming parser would otherwise have to handle around a
 * fence marker split across two deltas. An unclosed hidden fence (still
 * streaming) is hidden in its entirety, including the opening marker,
 * from the moment its "```lang\n" line is complete — there's necessarily
 * a few characters of the opening ``` itself visible for one delta before
 * that line completes, which is an acceptable, brief artifact next to the
 * alternative of flashing the whole JSON payload.
 */
export function filterFencedBlocks(raw: string, hiddenLangs: ReadonlySet<string>): string {
  if (hiddenLangs.size === 0) return raw
  const fenceOpen = /```([A-Za-z0-9_-]*)\n/g
  let out = ''
  let i = 0
  for (;;) {
    fenceOpen.lastIndex = i
    const m = fenceOpen.exec(raw)
    if (!m) {
      out += raw.slice(i)
      break
    }
    out += raw.slice(i, m.index)
    const lang = m[1]
    const bodyStart = m.index + m[0].length
    const closeIdx = raw.indexOf('```', bodyStart)
    if (!hiddenLangs.has(lang)) {
      // Not a renderer language: pass the whole block through as-is.
      if (closeIdx === -1) {
        out += raw.slice(m.index)
        break
      }
      out += raw.slice(m.index, closeIdx + 3)
      i = closeIdx + 3
      continue
    }
    if (closeIdx === -1) {
      // Still streaming the hidden block's body — nothing more to show
      // until either it closes (next call resolves it) or the render
      // card appears.
      break
    }
    i = closeIdx + 3
  }
  return out
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
  // rawTextByNode holds each node's unfiltered accumulated text — bubble.text
  // is always filterFencedBlocks(raw, ...) of this, recomputed on every
  // delta rather than incrementally, so a fence marker split across two
  // deltas is never a special case to handle.
  const rawTextByNode: Record<string, string> = {}
  const hiddenLangsByNode: Record<string, ReadonlySet<string>> = {}

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
        {
          const renderers = payload.renderers as Record<string, string[]> | undefined
          if (renderers) {
            for (const [n, langs] of Object.entries(renderers)) {
              hiddenLangsByNode[n] = new Set(langs)
            }
          }
        }
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
        rawTextByNode[node] = (rawTextByNode[node] ?? '') + text
        b.text = filterFencedBlocks(rawTextByNode[node], hiddenLangsByNode[node] ?? EMPTY_LANG_SET)
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
        if (text) {
          rawTextByNode[node] = text
          b.text = filterFencedBlocks(text, hiddenLangsByNode[node] ?? EMPTY_LANG_SET)
        }
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

      case 'node.render': {
        const plugin = typeof payload.plugin === 'string' ? payload.plugin : ''
        const version = typeof payload.version === 'string' ? payload.version : ''
        const renderer = typeof payload.renderer === 'string' ? payload.renderer : ''
        const resourceUri = typeof payload.resource_uri === 'string' ? payload.resource_uri : ''
        const entry = typeof payload.entry === 'string' ? payload.entry : ''
        if (plugin && version && renderer && resourceUri && entry) {
          entries.push({
            kind: 'render',
            key: `render-${ev.id}`,
            node,
            plugin,
            version,
            renderer,
            resourceUri,
            entry,
            data: payload.data,
          })
        }
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
