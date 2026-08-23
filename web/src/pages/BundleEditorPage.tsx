import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Connection,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { AgentPanel, AGENT_DRAG_MIME } from '@/components/bundle-editor/AgentPanel'
import { PropertiesPanel } from '@/components/bundle-editor/PropertiesPanel'
import { SourceView } from '@/components/bundle-editor/SourceView'
import { AgentNode, EndNode } from '@/components/bundle-editor/AgentNode'
import { BundleEdgeView, SelfLoopEdgeView } from '@/components/bundle-editor/BundleEdges'
import {
  definitionToGraph,
  graphToOrchestration,
  agentNodesToDefinitionAgents,
  annotateParallelEdges,
  END_NODE_ID,
  type AgentNode as AgentNodeT,
  type BundleEdge,
} from '@/lib/bundleEditor/graphIO'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Bundle = components['schemas']['Bundle']
type BundleDefinition = components['schemas']['BundleDefinition']

const nodeTypes = { agentNode: AgentNode, endNode: EndNode }
const edgeTypes = { bundleEdge: BundleEdgeView, selfLoop: SelfLoopEdgeView }

const BLANK_META = { bundle: '', version: '1.0', description: '' }
const BLANK_END = { id: END_NODE_ID, type: 'endNode' as const, position: { x: 480, y: 40 }, data: { ref: END_NODE_ID }, deletable: false }

interface ValidationIssue {
  target: string
  reason: string
}

function EditorInner() {
  const { ref } = useParams<{ ref?: string }>()
  const navigate = useNavigate()
  const isNarrow = useIsNarrowViewport()

  const [nodes, setNodes, onNodesChange] = useNodesState<AgentNodeT>([BLANK_END])
  const [edges, setEdges, onEdgesChange] = useEdgesState<BundleEdge>([])
  const [meta, setMeta] = useState(BLANK_META)
  const [entry, setEntry] = useState<string | null>(null)
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const [tab, setTab] = useState<'canvas' | 'source'>('canvas')
  const [sourceText, setSourceText] = useState('')
  const [sourceValid, setSourceValid] = useState(true)
  const [dirty, setDirty] = useState(false)
  const [issues, setIssues] = useState<ValidationIssue[]>([])
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const { screenToFlowPosition, fitView } = useReactFlow()
  const wrapperRef = useRef<HTMLDivElement>(null)

  // Load an existing Bundle for editing — no GET /bundles/{ref}, so pull
  // the list (which already carries the full definition) and filter.
  const existingQuery = useQuery({
    queryKey: ['bundles'],
    queryFn: async () => unwrap<{ items: Bundle[] }>(await apiClient.GET('/bundles', {})),
    enabled: !!ref,
  })

  useEffect(() => {
    if (!ref || !existingQuery.data) return
    const bundle = existingQuery.data.items.find((b) => b.bundle_ref === ref)
    if (!bundle) return
    const graph = definitionToGraph(bundle.definition)
    setNodes(graph.nodes)
    setEdges(annotateParallelEdges(graph.edges))
    setMeta({ bundle: graph.bundle, version: bumpVersion(graph.version), description: graph.description })
    setEntry(graph.entry)
    setDirty(false)
    requestAnimationFrame(() => fitView({ padding: 0.2 }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref, existingQuery.data])

  // Unsaved-changes guard.
  useEffect(() => {
    function onBeforeUnload(e: BeforeUnloadEvent) {
      if (dirty) e.preventDefault()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const markDirty = useCallback(() => setDirty(true), [])

  const onConnect = useCallback(
    (connection: Connection) => {
      setEdges((eds) =>
        annotateParallelEdges(
          addEdge<BundleEdge>(
            { ...connection, type: connection.source === connection.target ? 'selfLoop' : 'bundleEdge', data: {} },
            eds,
          ),
        ),
      )
      markDirty()
    },
    [setEdges, markDirty],
  )

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      const raw = e.dataTransfer.getData(AGENT_DRAG_MIME)
      if (!raw) return
      const agent = JSON.parse(raw) as { ref: string; version: string }
      const position = screenToFlowPosition({ x: e.clientX, y: e.clientY })

      let id = agent.ref
      let n = 2
      const existingIds = new Set(nodes.map((x) => x.id))
      while (existingIds.has(id)) {
        id = `${agent.ref}_${n}`
        n++
      }
      const alias = id !== agent.ref ? id : undefined

      const newNode: AgentNodeT = {
        id,
        type: 'agentNode',
        position,
        data: { ref: agent.ref, alias, version: agent.version },
      }
      setNodes((nds) => [...nds, newNode])
      setSelectedNodeId(id)
      if (!entry) setEntry(id)
      markDirty()
    },
    [nodes, screenToFlowPosition, setNodes, entry, markDirty],
  )

  function updateNodeData(id: string, patch: Partial<AgentNodeT['data']>) {
    setNodes((nds) => nds.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n)))
    markDirty()
  }

  function updateEdgeData(id: string, patch: Partial<BundleEdge['data']>) {
    setEdges((eds) => annotateParallelEdges(eds.map((e) => (e.id === id ? { ...e, data: { ...e.data, ...patch } } : e))))
    markDirty()
  }

  const nodeNames = useMemo(() => nodes.filter((n) => n.type === 'agentNode').map((n) => n.id), [nodes])
  const selectedNode = nodes.find((n) => n.id === selectedNodeId) ?? null
  const selectedEdge = edges.find((e) => e.id === selectedEdgeId) ?? null

  function buildDefinition(): BundleDefinition {
    return {
      bundle: meta.bundle,
      version: meta.version,
      description: meta.description || undefined,
      agents: agentNodesToDefinitionAgents(nodes),
      orchestration: graphToOrchestration(nodes, edges, entry),
    }
  }

  function switchToSource() {
    setSourceText(JSON.stringify(buildDefinition(), null, 2))
    setTab('source')
  }

  function switchToCanvas() {
    if (!sourceValid) return
    try {
      const def = JSON.parse(sourceText) as BundleDefinition
      const graph = definitionToGraph(def)
      setNodes(graph.nodes)
      setEdges(annotateParallelEdges(graph.edges))
      setMeta({ bundle: graph.bundle, version: graph.version, description: graph.description })
      setEntry(graph.entry)
      setTab('canvas')
      markDirty()
      requestAnimationFrame(() => fitView({ padding: 0.2 }))
    } catch {
      // JSON parses but doesn't match the expected shape — SourceView
      // already gates on JSON.parse validity; a shape mismatch surfaces
      // at save time via the backend's own schema validation instead.
      setTab('canvas')
    }
  }

  async function save() {
    setSaving(true)
    setSaveError(null)
    setIssues([])
    setNodes((nds) => nds.map((n) => ({ ...n, data: { ...n.data, invalid: false } })))
    setEdges((eds) => eds.map((e) => ({ ...e, data: { ...e.data, invalid: false } })))
    try {
      const definition = tab === 'source' ? (JSON.parse(sourceText) as BundleDefinition) : buildDefinition()
      unwrap(
        await apiClient.POST('/bundles', {
          body: { definition },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      toast.success('已保存')
      setDirty(false)
      navigate('/apps')
    } catch (err) {
      if (err instanceof ApiError && err.details) {
        const parsed = err.details.map((d) => ({ target: d.field, reason: d.reason }))
        setIssues(parsed)
        highlightIssues(parsed)
      } else {
        setSaveError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
      }
    } finally {
      setSaving(false)
    }
  }

  function highlightIssues(list: ValidationIssue[]) {
    const mentionedNodes = new Set(nodeNames.filter((n) => list.some((i) => i.reason.includes(n) || i.target.includes(n))))
    setNodes((nds) => nds.map((n) => (mentionedNodes.has(n.id) ? { ...n, data: { ...n.data, invalid: true } } : n)))
    setEdges((eds) =>
      eds.map((e) =>
        mentionedNodes.has(e.source) && mentionedNodes.has(e.target) ? { ...e, data: { ...e.data, invalid: true } } : e,
      ),
    )
  }

  function focusIssue(target: string) {
    const node = nodes.find((n) => n.id === target || target.includes(n.id))
    if (node) {
      setSelectedNodeId(node.id)
      setSelectedEdgeId(null)
      fitView({ nodes: [{ id: node.id }], padding: 1, duration: 300 })
    }
  }

  if (isNarrow) {
    return (
      <div className="flex flex-col items-center gap-space-4 rounded-lg border border-dashed border-border py-space-11 text-center">
        <p className="text-label-md text-ink-900">请在更大屏幕上编辑编排图</p>
        <p className="text-body-sm max-w-[420px] text-ink-500">
          拖拽式编排编辑器需要横向空间，在小屏幕上勉强适配会得到一个难用的体验。
        </p>
      </div>
    )
  }

  return (
    <div className="flex h-[calc(100vh-160px)] flex-col gap-space-3">
      <div className="flex items-center justify-between">
        <Tabs value={tab} onValueChange={(v) => (v === 'source' ? switchToSource() : switchToCanvas())}>
          <TabsList>
            <TabsTrigger value="canvas">画布</TabsTrigger>
            <TabsTrigger value="source">DSL 源码</TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="flex items-center gap-space-3">
          {dirty && <span className="text-caption text-[var(--color-warning)]">有未保存的更改</span>}
          <Button disabled={saving || (tab === 'source' && !sourceValid)} onClick={save}>
            {saving ? '保存中…' : '保存并校验'}
          </Button>
        </div>
      </div>

      {saveError && (
        <p role="alert" className="text-body-sm rounded-md border border-[var(--color-error)] px-space-4 py-space-2 text-[var(--color-error)]">
          {saveError}
        </p>
      )}

      <div className="grid flex-1 grid-cols-[240px_1fr_300px] overflow-hidden rounded-lg border border-border">
        <AgentPanel />

        {tab === 'canvas' ? (
          <div
            ref={wrapperRef}
            className="relative bg-surface-muted"
            onDrop={onDrop}
            onDragOver={(e) => e.preventDefault()}
          >
            {nodes.length <= 1 && (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="rounded-lg border border-dashed border-border bg-surface/90 px-space-6 py-space-5 text-center">
                  <p className="text-label-md text-ink-900">← 从左侧拖入第一个 Agent 开始编排</p>
                </div>
              </div>
            )}
            <ReactFlow
              nodes={nodes}
              edges={edges}
              onNodesChange={(changes) => {
                onNodesChange(changes)
                markDirty()
              }}
              onEdgesChange={(changes) => {
                onEdgesChange(changes)
                markDirty()
              }}
              onConnect={onConnect}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              deleteKeyCode={['Delete', 'Backspace']}
              onSelectionChange={({ nodes: sel, edges: selE }) => {
                setSelectedNodeId(sel[0]?.id ?? null)
                setSelectedEdgeId(selE[0]?.id ?? null)
              }}
              fitView
            >
              <Background gap={20} />
              <Controls />
            </ReactFlow>
          </div>
        ) : (
          <SourceView value={sourceText} onChange={setSourceText} onValidChange={setSourceValid} />
        )}

        <PropertiesPanel
          selectedNode={selectedNode}
          selectedEdge={selectedEdge}
          nodeNames={nodeNames}
          onUpdateNode={updateNodeData}
          onUpdateEdge={updateEdgeData}
          bundleMeta={meta}
          onUpdateMeta={(patch) => {
            setMeta((m) => ({ ...m, ...patch }))
            markDirty()
          }}
          entry={entry}
          onSetEntry={(id) => {
            setEntry(id)
            markDirty()
          }}
          issues={issues}
          onFocusIssue={focusIssue}
        />
      </div>
    </div>
  )
}

function bumpVersion(v: string): string {
  const parts = v.split('.').map(Number)
  parts[parts.length - 1] = (parts[parts.length - 1] || 0) + 1
  return parts.join('.')
}

function useIsNarrowViewport() {
  const [narrow, setNarrow] = useState(() => window.innerWidth <= 900)
  useEffect(() => {
    function onResize() {
      setNarrow(window.innerWidth <= 900)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])
  return narrow
}

export function BundleEditorPage() {
  return (
    <ReactFlowProvider>
      <EditorInner />
    </ReactFlowProvider>
  )
}
