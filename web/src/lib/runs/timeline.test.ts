import { describe, expect, it } from 'vitest'

import { buildTimeline } from '@/lib/runs/timeline'
import type { RunEvent } from '@/lib/runs/useRunEvents'

function ev(id: number, type: RunEvent['type'], node?: string, payload?: unknown): RunEvent {
  return { id, type, run_id: 'run-1', node, timestamp: new Date(id).toISOString(), payload }
}

describe('buildTimeline', () => {
  it('accumulates node.thinking chunks into one bubble with a typewriter-style append', () => {
    const { bubbles, entries } = buildTimeline([
      ev(1, 'bundle.started'),
      ev(2, 'node.thinking', 'architect', { text: 'Hello' }),
      ev(3, 'node.thinking', 'architect', { text: ', world' }),
      ev(4, 'node.finished', 'architect', { text: 'Hello, world!' }),
    ])
    expect(bubbles.architect.text).toBe('Hello, world!')
    expect(bubbles.architect.status).toBe('done')
    expect(bubbles.architect.isBusy).toBe(false)
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'bubble-group', nodes: ['architect'] })
  })

  it('groups two nodes running concurrently into one bubble-group, per the parallel-agent rule', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.thinking', 'ui_designer', { text: 'designing' }),
      ev(2, 'node.thinking', 'fullstack_engineer', { text: 'coding' }),
      ev(3, 'node.finished', 'ui_designer', { text: 'done' }),
      ev(4, 'node.finished', 'fullstack_engineer', { text: 'done' }),
    ])
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ kind: 'bubble-group', nodes: ['ui_designer', 'fullstack_engineer'] })
  })

  it('starts a new group once the previous group has fully finished', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.finished', 'a', { text: 'a done' }),
      ev(2, 'node.thinking', 'b', { text: 'b thinking' }),
    ])
    expect(entries).toHaveLength(2)
    expect(entries[0]).toMatchObject({ kind: 'bubble-group', nodes: ['a'] })
    expect(entries[1]).toMatchObject({ kind: 'bubble-group', nodes: ['b'] })
  })

  it('tracks tool call start/finish pairs on the owning bubble', () => {
    const { bubbles } = buildTimeline([
      ev(1, 'node.tool_call.started', 'architect', { name: 'web_search' }),
      ev(2, 'node.tool_call.finished', 'architect', { name: 'web_search', result: {} }),
    ])
    expect(bubbles.architect.toolCalls).toEqual([{ name: 'web_search', status: 'finished' }])
  })

  it('inserts an inline gate entry on human_gate.waiting and resolves it in place', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.finished', 'architect', {}),
      ev(2, 'human_gate.waiting', 'architect', { gate_id: 7 }),
      ev(3, 'human_gate.resolved', 'architect', { status: 'approved' }),
    ])
    const gate = entries.find((e) => e.kind === 'gate')
    expect(gate).toMatchObject({ kind: 'gate', node: 'architect', gateId: 7, status: 'approved' })
  })

  it('closes the open bubble group when a gate is inserted, so later nodes start fresh', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.thinking', 'architect', { text: 'thinking' }),
      ev(2, 'human_gate.waiting', 'architect', { gate_id: 1 }),
      ev(3, 'node.thinking', 'fullstack_engineer', { text: 'thinking' }),
    ])
    expect(entries.map((e) => e.kind)).toEqual(['bubble-group', 'gate', 'bubble-group'])
  })

  it('surfaces bundle.failed as a terminal system entry with the sanitized error text', () => {
    const { entries, runStatus, runError } = buildTimeline([
      ev(1, 'bundle.started'),
      ev(2, 'bundle.failed', undefined, { error: '所有模型 Provider 均不可用' }),
    ])
    expect(runStatus).toBe('failed')
    expect(runError).toBe('所有模型 Provider 均不可用')
    expect(entries.at(-1)).toMatchObject({ kind: 'system', tone: 'error' })
  })
})
