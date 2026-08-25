import { describe, expect, it } from 'vitest'

import { definitionToGraph, graphToOrchestration, agentNodesToDefinitionAgents, END_NODE_ID } from '@/lib/bundleEditor/graphIO'
import type { components } from '@/lib/api/schema'

type BundleDefinition = components['schemas']['BundleDefinition']

const HANDWRITTEN: BundleDefinition = {
  type: 'graph',
  bundle: 'web-app-builder',
  version: '1.0',
  description: 'test',
  agents: [{ ref: 'product_manager' }, { ref: 'architect' }, { ref: 'ui_designer' }, { ref: 'fullstack_engineer' }],
  orchestration: {
    mode: 'graph',
    entry: 'product_manager',
    edges: [
      { from: 'product_manager', to: 'architect' },
      { from: 'architect', to: ['ui_designer', 'fullstack_engineer'], mode: 'parallel' },
      { from: 'ui_designer', to: 'fullstack_engineer', join: 'wait_all' },
      { from: 'fullstack_engineer', to: 'END', condition: 'shared_state.tests_passed == true' },
      { from: 'fullstack_engineer', to: 'fullstack_engineer', condition: 'shared_state.tests_passed == false' },
    ],
    human_gates: [{ after: 'architect', timeout_seconds: 1800, on_timeout: 'abort' }],
  },
}

describe('definitionToGraph / graphToOrchestration round trip', () => {
  it('produces an orchestration equivalent to the handwritten DSL', () => {
    const graph = definitionToGraph(HANDWRITTEN)

    // Every declared agent became a node, plus the fixed END node.
    expect(graph.nodes.map((n) => n.id).sort()).toEqual(
      ['product_manager', 'architect', 'ui_designer', 'fullstack_engineer', END_NODE_ID].sort(),
    )
    expect(graph.entry).toBe('product_manager')

    const orch = graphToOrchestration(graph.nodes, graph.edges, graph.entry)

    // Same edge count and content, order-independent.
    const normalize = (e: { from: string; to: string | string[]; condition?: string; join?: string; mode?: string }) => ({
      from: e.from,
      to: Array.isArray(e.to) ? [...e.to].sort() : e.to,
      condition: e.condition ?? null,
      join: e.join ?? null,
      parallel: e.mode === 'parallel',
    })
    const want = HANDWRITTEN.orchestration!.edges.map(normalize)
    const got = orch.edges.map(normalize)
    expect(got.sort((a, b) => a.from.localeCompare(b.from))).toEqual(want.sort((a, b) => a.from.localeCompare(b.from)))

    expect(orch.human_gates).toEqual([{ after: 'architect', timeout_seconds: 1800, on_timeout: 'abort' }])

    const agents = agentNodesToDefinitionAgents(graph.nodes)
    expect(agents.map((a) => a.ref).sort()).toEqual(['architect', 'fullstack_engineer', 'product_manager', 'ui_designer'].sort())
  })

  it('recognizes a fan-out from one source as a parallel entry', () => {
    const graph = definitionToGraph(HANDWRITTEN)
    const orch = graphToOrchestration(graph.nodes, graph.edges, graph.entry)
    const fanOut = orch.edges.find((e) => e.from === 'architect')
    expect(fanOut).toMatchObject({ mode: 'parallel' })
    expect(Array.isArray(fanOut?.to) ? [...(fanOut!.to as string[])].sort() : null).toEqual(['fullstack_engineer', 'ui_designer'])
  })

  it('marks a self-loop edge distinctly (selfLoop edge type) for the retry-edge visual', () => {
    const graph = definitionToGraph(HANDWRITTEN)
    const loop = graph.edges.find((e) => e.source === 'fullstack_engineer' && e.target === 'fullstack_engineer')
    expect(loop?.type).toBe('selfLoop')
    expect(loop?.data?.condition).toBe('shared_state.tests_passed == false')
  })

  it('keeps a conditioned edge un-grouped even when its source has other conditionless edges', () => {
    const graph = definitionToGraph(HANDWRITTEN)
    const orch = graphToOrchestration(graph.nodes, graph.edges, graph.entry)
    const toEnd = orch.edges.find((e) => e.from === 'fullstack_engineer' && e.to === 'END')
    expect(toEnd?.condition).toBe('shared_state.tests_passed == true')
  })
})
