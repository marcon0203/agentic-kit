import { describe, expect, it } from 'vitest'

import { buildTimeline, filterFencedBlocks } from '@/lib/runs/timeline'
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

  it('inserts a render entry when a node.render event carries the full payload', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.thinking', 'analyst', { text: 'thinking' }),
      ev(2, 'node.render', 'analyst', {
        plugin: 'acme.charts',
        version: '1.0.0',
        renderer: 'chart',
        resource_uri: 'ui://acme.charts/chart',
        entry: 'ui/chart.html',
        data: { lang: 'chart', content: '{}' },
      }),
    ])
    expect(entries.at(-1)).toMatchObject({
      kind: 'render',
      node: 'analyst',
      plugin: 'acme.charts',
      version: '1.0.0',
      renderer: 'chart',
      resourceUri: 'ui://acme.charts/chart',
      entry: 'ui/chart.html',
    })
  })

  it('drops a node.render event missing a required payload field', () => {
    const { entries } = buildTimeline([
      ev(1, 'node.render', 'analyst', { plugin: 'acme.charts', renderer: 'chart' }),
    ])
    expect(entries.some((e) => e.kind === 'render')).toBe(false)
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

  it('hides a renderer-eligible fenced block from the live-streamed bubble text, keeping the surrounding prose', () => {
    const { bubbles } = buildTimeline([
      ev(1, 'bundle.started', undefined, { renderers: { analyst: ['chart'] } }),
      ev(2, 'node.thinking', 'analyst', { text: '这是月度销量：\n```chart\n{"labels"' }),
      ev(3, 'node.thinking', 'analyst', { text: ': ["一月"], "datasets": []}\n```\n供参考。' }),
    ])
    expect(bubbles.analyst.text).toBe('这是月度销量：\n\n供参考。')
  })

  it('leaves an ordinary fenced block (not a registered renderer language) visible as-is', () => {
    const { bubbles } = buildTimeline([
      ev(1, 'bundle.started', undefined, { renderers: { analyst: ['chart'] } }),
      ev(2, 'node.thinking', 'analyst', { text: '```python\nprint(1)\n```' }),
    ])
    expect(bubbles.analyst.text).toBe('```python\nprint(1)\n```')
  })

  it('also strips the fenced block from the final node.finished text, so it never lingers after the render card appears', () => {
    const { bubbles } = buildTimeline([
      ev(1, 'bundle.started', undefined, { renderers: { analyst: ['chart'] } }),
      ev(2, 'node.finished', 'analyst', { text: '图表如下：\n```chart\n{"labels": []}\n```' }),
    ])
    expect(bubbles.analyst.text).toBe('图表如下：\n')
  })
})

describe('filterFencedBlocks', () => {
  it('passes text through unchanged when no languages are hidden', () => {
    expect(filterFencedBlocks('```chart\n{}\n```', new Set())).toBe('```chart\n{}\n```')
  })

  it('hides an unclosed hidden-language fence entirely (still streaming)', () => {
    expect(filterFencedBlocks('before\n```chart\n{"partial":', new Set(['chart']))).toBe('before\n')
  })

  it('hides a closed hidden-language fence, keeping text before and after it', () => {
    expect(filterFencedBlocks('a\n```chart\n{}\n```\nb', new Set(['chart']))).toBe('a\n\nb')
  })
})
