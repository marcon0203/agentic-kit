import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Connection,
  type NodeChange,
  type EdgeChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ArrowLeft } from 'lucide-react'

import { TabRail, TabRailItem } from '@/components/common/Page'
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
  type BundleRunType,
} from '@/lib/bundleEditor/graphIO'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Bundle = components['schemas']['Bundle']
type BundleDefinition = components['schemas']['BundleDefinition']

const nodeTypes = { agentNode: AgentNode, endNode: EndNode }
const edgeTypes = { bundleEdge: BundleEdgeView, selfLoop: SelfLoopEdgeView }

const BLANK_META = { bundle: '', version: '1.0', description: '', runType: 'graph' as BundleRunType }
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
    setMeta({ bundle: graph.bundle, version: bumpVersion(graph.version), description: graph.description, runType: graph.runType })
    setEntry(graph.entry)
    setDirty(false)
    requestAnimationFrame(() => fitView({ padding: 0.2 }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref, existingQuery.data])

  // Whatever removed a node — the panel's 删除, the Delete key, a source-view
  // round-trip — the entry must not be left pointing at something that is no
  // longer on the canvas. A graph Bundle without a valid entry doesn't
  // compile, and the old failure mode was a save-time error message about a
  // node the user had already forgotten deleting.
  useEffect(() => {
    if (!entry) return
    if (nodes.some((n) => n.id === entry)) return
    const firstAgent = nodes.find((n) => n.type === 'agentNode')
    setEntry(firstAgent?.id ?? null)
  }, [nodes, entry])

  // Unsaved-changes guard.
  useEffect(() => {
    function onBeforeUnload(e: BeforeUnloadEvent) {
      if (dirty) e.preventDefault()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const markDirty = useCallback(() => setDirty(true), [])

  // Selecting a node, or React Flow measuring one on mount, is not an edit.
  // Marking dirty on every change put "有未保存的更改" on screen the moment
  // the editor opened — which trains people to ignore the warning that is
  // supposed to stop them losing work.
  const isRealEdit = useCallback(
    (changes: NodeChange<AgentNodeT>[] | EdgeChange<BundleEdge>[]) =>
      changes.some((c) => c.type !== 'select' && c.type !== 'dimensions'),
    [],
  )

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

  const addAgent = useCallback(
    (agent: { ref: string; version: string }, position?: { x: number; y: number }) => {
      let id = agent.ref
      let n = 2
      const existingIds = new Set(nodes.map((x) => x.id))
      while (existingIds.has(id)) {
        id = `${agent.ref}_${n}`
        n++
      }
      const alias = id !== agent.ref ? id : undefined

      // Clicking rather than dragging lands the node in open space near the
      // last one instead of stacking every click on the exact same spot.
      const fallback = { x: 120 + nodes.length * 40, y: 120 + nodes.length * 70 }

      const newNode: AgentNodeT = {
        id,
        type: 'agentNode',
        position: position ?? fallback,
        data: { ref: agent.ref, alias, version: agent.version },
      }
      setNodes((nds) => [...nds, newNode])
      setSelectedNodeId(id)
      if (!entry) setEntry(id)
      markDirty()
    },
    [nodes, setNodes, entry, markDirty],
  )

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      const raw = e.dataTransfer.getData(AGENT_DRAG_MIME)
      if (!raw) return
      const agent = JSON.parse(raw) as { ref: string; version: string }
      addAgent(agent, screenToFlowPosition({ x: e.clientX, y: e.clientY }))
    },
    [addAgent, screenToFlowPosition],
  )

  function updateNodeData(id: string, patch: Partial<AgentNodeT['data']>) {
    setNodes((nds) => nds.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n)))
    markDirty()
  }

  function updateEdgeData(id: string, patch: Partial<BundleEdge['data']>) {
    setEdges((eds) => annotateParallelEdges(eds.map((e) => (e.id === id ? { ...e, data: { ...e.data, ...patch } } : e))))
    markDirty()
  }

  function deleteNode(id: string) {
    setNodes((nds) => nds.filter((n) => n.id !== id))
    // A dangling edge would serialize into an orchestration referencing a
    // node that no longer exists, which only surfaces as a save-time
    // validation error well after the delete.
    setEdges((eds) => annotateParallelEdges(eds.filter((e) => e.source !== id && e.target !== id)))
    if (selectedNodeId === id) setSelectedNodeId(null)
    markDirty()
  }

  function deleteEdge(id: string) {
    setEdges((eds) => annotateParallelEdges(eds.filter((e) => e.id !== id)))
    if (selectedEdgeId === id) setSelectedEdgeId(null)
    markDirty()
  }

  const nodeNames = useMemo(() => nodes.filter((n) => n.type === 'agentNode').map((n) => n.id), [nodes])
  const selectedNode = nodes.find((n) => n.id === selectedNodeId) ?? null
  const selectedEdge = edges.find((e) => e.id === selectedEdgeId) ?? null

  // entry is Bundle-level state, but it's a fact about one node, so the
  // canvas gets told which node it is rather than leaving that answer only
  // in the properties panel's dropdown.
  const renderNodes = useMemo(
    () => nodes.map((n) => (n.type === 'agentNode' ? { ...n, data: { ...n.data, isEntry: n.id === entry } } : n)),
    [nodes, entry],
  )

  function buildDefinition(): BundleDefinition {
    const base = {
      bundle: meta.bundle,
      version: meta.version,
      description: meta.description || undefined,
    }
    const agents = agentNodesToDefinitionAgents(nodes)

    // The run type is a real dispatch difference, not just a DSL shape
    // choice — flow/single never carry an orchestration block at all,
    // since neither has edges to walk.
    switch (meta.runType) {
      case 'flow':
        return { ...base, type: 'flow', agents }
      case 'single':
        return { ...base, type: 'single', agents: agents.slice(0, 1) }
      default:
        return { ...base, type: 'graph', agents, orchestration: graphToOrchestration(nodes, edges, entry) }
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
      setMeta({ bundle: graph.bundle, version: graph.version, description: graph.description, runType: graph.runType })
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
      navigate('/apps/bundles')
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

  // beforeunload only covers closing the tab — clicking 返回 is an in-app
  // navigation the browser never sees, and it silently threw the whole graph
  // away.
  function leave() {
    if (dirty && !window.confirm('有未保存的更改，确定离开吗？')) return
    navigate('/apps/bundles')
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
        <div className="flex min-w-0 items-center gap-space-4">
          <Button variant="ghost" size="sm" onClick={leave}>
            <ArrowLeft className="size-4" aria-hidden />
            返回
          </Button>
          {/* Which app is open, and whether this is a new one or a new
              version of an existing one — the old header showed neither, so
              an edit was indistinguishable from a create. */}
          <span className="flex min-w-0 items-baseline gap-space-2">
            <span className="text-label-md truncate text-ink-900">
              {ref ? `编辑 ${ref}` : '新建应用'}
            </span>
            {ref && <span className="text-caption tabular text-ink-500">将保存为 v{meta.version}</span>}
          </span>
          <TabRail className="border-b-0">
            <TabRailItem active={tab === 'canvas'} onClick={switchToCanvas}>
              画布
            </TabRailItem>
            <TabRailItem active={tab === 'source'} onClick={switchToSource}>
              DSL 源码
            </TabRailItem>
          </TabRail>
        </div>

        <div className="flex items-center gap-space-3">
          {dirty && <span className="text-caption text-signal">有未保存的更改</span>}
          <Button disabled={saving || (tab === 'source' && !sourceValid)} onClick={save}>
            {saving ? '保存中…' : '保存并校验'}
          </Button>
        </div>
      </div>

      {saveError && (
        <p role="alert" className="text-body-sm rounded-md border border-rust px-space-4 py-space-2 text-rust">
          {saveError}
        </p>
      )}

      <div className="grid flex-1 grid-cols-[240px_1fr_300px] overflow-hidden rounded-lg border border-border">
        <AgentPanel onAdd={(agent) => addAgent(agent)} />

        {tab === 'canvas' ? (
          <div
            ref={wrapperRef}
            className="relative bg-surface-muted"
            onDrop={onDrop}
            onDragOver={(e) => e.preventDefault()}
          >
            {nodes.length <= 1 && (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="flex flex-col items-center gap-space-3 rounded-lg border border-border bg-surface px-space-7 py-space-6 text-center">
                  {/* An empty rail: three stations with nothing on them yet,
                      the same mark used everywhere else for "not here yet". */}
                  <span aria-hidden className="flex w-[180px] items-center">
                    <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
                    <span className="h-px flex-1 bg-border" />
                    <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
                    <span className="h-px flex-1 bg-border" />
                    <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
                  </span>
                  <p className="text-display-sm text-ink-900">从左侧拖一个 Agent 进来</p>
                  <p className="text-body-sm max-w-[34ch] text-ink-700">
                    拖进来的 Agent 是图上的节点；把它们连起来就定义了执行顺序。
                  </p>
                </div>
              </div>
            )}
            <ReactFlow
              nodes={renderNodes}
              edges={edges}
              onNodesChange={(changes) => {
                onNodesChange(changes)
                if (isRealEdit(changes)) markDirty()
              }}
              onEdgesChange={(changes) => {
                onEdgesChange(changes)
                if (isRealEdit(changes)) markDirty()
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
              <Background gap={22} color="var(--color-border-strong)" />
              <Controls />
              {/* Past a handful of nodes the canvas scrolls off-screen and
                  panning becomes guesswork without an overview. */}
              <MiniMap pannable zoomable className="!bg-surface" maskColor="var(--color-surface-muted)" />
            </ReactFlow>

            {/* The canvas has real keyboard affordances (delete, multi-select,
                self-loop) that were previously undiscoverable. */}
            <div className="text-caption pointer-events-none absolute bottom-space-3 left-space-3 z-10 flex flex-wrap items-center gap-space-3 rounded-sm bg-surface/90 px-space-3 py-space-2 text-ink-500 shadow-sm">
              <span>
                <Kbd>Delete</Kbd> 删除选中
              </span>
              <span>
                <Kbd>Shift</Kbd> + 拖拽 框选
              </span>
              <span>拖动节点右侧圆点连线；连回自己 = 自循环重试</span>
            </div>
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
          onUpdateRunType={(runType) => {
            setMeta((m) => ({ ...m, runType }))
            markDirty()
          }}
          entry={entry}
          onSetEntry={(id) => {
            setEntry(id)
            markDirty()
          }}
          onDeleteNode={deleteNode}
          onDeleteEdge={deleteEdge}
          issues={issues}
          onFocusIssue={focusIssue}
        />
      </div>
    </div>
  )
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="text-caption rounded-xs border border-border-strong bg-surface-page px-1 py-0.5 font-mono text-ink-700">
      {children}
    </kbd>
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
