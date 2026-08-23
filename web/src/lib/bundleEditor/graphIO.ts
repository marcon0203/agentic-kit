import type { Node, Edge } from '@xyflow/react'
import type { components } from '@/lib/api/schema'

type BundleDefinition = components['schemas']['BundleDefinition']
type OrchestrationEdge = NonNullable<BundleDefinition['orchestration']>['edges'][number]
type HumanGate = NonNullable<NonNullable<BundleDefinition['orchestration']>['human_gates']>[number]

export const END_NODE_ID = 'END'

export interface AgentNodeData {
  ref: string
  alias?: string
  version?: string
  gate?: { timeout_seconds?: number; on_timeout: 'auto_approve' | 'auto_reject' | 'abort' }
  invalid?: boolean
  [key: string]: unknown
}

export interface EdgeData {
  condition?: string
  join?: 'wait_all' | 'wait_any'
  invalid?: boolean
  invalidReason?: string
  /** Computed, not stored — true when this edge's source has more than
   * one conditionless outgoing edge (spec-17's fan-out "并行" chip). */
  parallel?: boolean
  [key: string]: unknown
}

/** Annotates edges with `data.parallel` — call after any edit that could
 * change fan-out membership (add/remove edge, add/remove condition)
 * before handing edges to React Flow for rendering. */
export function annotateParallelEdges(edges: BundleEdge[]): BundleEdge[] {
  const countBySource = new Map<string, number>()
  for (const e of edges) {
    if (e.data?.condition) continue
    countBySource.set(e.source, (countBySource.get(e.source) ?? 0) + 1)
  }
  return edges.map((e) => ({
    ...e,
    data: { ...e.data, parallel: !e.data?.condition && (countBySource.get(e.source) ?? 0) > 1 },
  }))
}

export type AgentNode = Node<AgentNodeData>
export type BundleEdge = Edge<EdgeData>

export interface BundleGraph {
  bundle: string
  version: string
  description: string
  entry: string | null
  nodes: AgentNode[]
  edges: BundleEdge[]
}

function nodeNameOf(a: { ref: string; alias?: string }): string {
  return a.alias || a.ref
}

/** Reduces a BundleDefinition's orchestration into React Flow nodes/edges —
 * the inverse of `graphToOrchestration`. Node positions aren't part of the
 * DSL, so they're assigned by a simple left-to-right BFS layer layout on
 * import (re-editing an existing Bundle starts from a readable layout,
 * not everything stacked at the origin). */
export function definitionToGraph(def: BundleDefinition): BundleGraph {
  const agents = def.agents ?? []
  const orch = def.orchestration
  const edgesDsl = orch?.edges ?? []
  const gates = orch?.human_gates ?? []

  const gateByNode = new Map<string, HumanGate>()
  for (const g of gates) gateByNode.set(g.after, g)

  const rfEdges: BundleEdge[] = []
  edgesDsl.forEach((e, i) => {
    const targets = Array.isArray(e.to) ? e.to : [e.to]
    targets.forEach((to, j) => {
      rfEdges.push({
        id: `e-${i}-${j}`,
        source: e.from,
        target: to,
        type: e.from === to ? 'selfLoop' : 'bundleEdge',
        data: { condition: e.condition, join: e.join as EdgeData['join'] },
      })
    })
  })

  // BFS layering from entry for an initial readable layout.
  const adjacency = new Map<string, string[]>()
  for (const e of rfEdges) {
    if (!adjacency.has(e.source)) adjacency.set(e.source, [])
    adjacency.get(e.source)!.push(e.target)
  }
  const layer = new Map<string, number>()
  const entry = orch?.entry ?? nodeNameOf(agents[0] ?? { ref: '' })
  if (entry) layer.set(entry, 0)
  const queue = entry ? [entry] : []
  while (queue.length > 0) {
    const cur = queue.shift()!
    const curLayer = layer.get(cur) ?? 0
    for (const next of adjacency.get(cur) ?? []) {
      if (next === cur) continue // self-loop doesn't advance layer
      if (!layer.has(next) || layer.get(next)! < curLayer + 1) {
        layer.set(next, curLayer + 1)
        queue.push(next)
      }
    }
  }

  const countPerLayer = new Map<number, number>()
  const nodes: AgentNode[] = agents.map((a) => {
    const id = nodeNameOf(a)
    const l = layer.get(id) ?? 0
    const yIndex = countPerLayer.get(l) ?? 0
    countPerLayer.set(l, yIndex + 1)
    const gate = gateByNode.get(id)
    return {
      id,
      type: 'agentNode',
      position: { x: l * 220 + 40, y: yIndex * 140 + 40 },
      data: {
        ref: a.ref,
        alias: a.alias,
        version: a.version,
        gate: gate
          ? { timeout_seconds: gate.timeout_seconds, on_timeout: (gate.on_timeout ?? 'abort') as 'auto_approve' | 'auto_reject' | 'abort' }
          : undefined,
      },
    }
  })

  const endLayer = Math.max(0, ...[...layer.values()]) + 1
  nodes.push({
    id: END_NODE_ID,
    type: 'endNode',
    position: { x: endLayer * 220 + 40, y: 40 },
    data: { ref: END_NODE_ID },
    deletable: false,
  })

  return {
    bundle: def.bundle ?? '',
    version: def.version ?? '1.0',
    description: def.description ?? '',
    entry: entry || null,
    nodes,
    edges: rfEdges,
  }
}

/** The inverse of definitionToGraph — groups same-source edges that share
 * no condition into one fan-out entry (spec-17: "一对多连线自动识别为并行
 * 分支"); everything else (conditioned edges, self-loops) stays one DSL
 * entry per edge so each keeps its own condition/join. */
export function graphToOrchestration(nodes: AgentNode[], edges: BundleEdge[], entry: string | null) {
  const bySource = new Map<string, BundleEdge[]>()
  for (const e of edges) {
    if (!bySource.has(e.source)) bySource.set(e.source, [])
    bySource.get(e.source)!.push(e)
  }

  const dslEdges: OrchestrationEdge[] = []
  for (const [source, group] of bySource) {
    const conditionless = group.filter((e) => !e.data?.condition)
    const conditioned = group.filter((e) => e.data?.condition)

    if (conditionless.length > 1) {
      dslEdges.push({
        from: source,
        to: conditionless.map((e) => e.target),
        mode: 'parallel',
        ...(conditionless.find((e) => e.data?.join)?.data?.join ? { join: conditionless.find((e) => e.data?.join)!.data!.join } : {}),
      })
    } else {
      for (const e of conditionless) {
        dslEdges.push({ from: source, to: e.target, ...(e.data?.join ? { join: e.data.join } : {}) })
      }
    }
    for (const e of conditioned) {
      dslEdges.push({
        from: source,
        to: e.target,
        condition: e.data!.condition,
        ...(e.data?.join ? { join: e.data.join } : {}),
      })
    }
  }

  const humanGates: HumanGate[] = nodes
    .filter((n) => n.type === 'agentNode' && n.data.gate)
    .map((n) => ({
      after: n.id,
      ...(n.data.gate!.timeout_seconds ? { timeout_seconds: n.data.gate!.timeout_seconds } : {}),
      on_timeout: n.data.gate!.on_timeout,
    }))

  return {
    mode: 'graph' as const,
    entry: entry ?? '',
    edges: dslEdges,
    ...(humanGates.length > 0 ? { human_gates: humanGates } : {}),
  }
}

export function agentNodesToDefinitionAgents(nodes: AgentNode[]): BundleDefinition['agents'] {
  return nodes
    .filter((n) => n.type === 'agentNode')
    .map((n) => ({ ref: n.data.ref, ...(n.data.alias ? { alias: n.data.alias } : {}), ...(n.data.version ? { version: n.data.version } : {}) }))
}
