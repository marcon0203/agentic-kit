import { useState } from 'react'
import { Lock } from 'lucide-react'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { StatusChip, type PlatformStatus } from '@/components/run/StatusChip'
import type { NodeBubbleState } from '@/lib/runs/timeline'
import type { RunEvent } from '@/lib/runs/useRunEvents'

function nodeStatus(b: NodeBubbleState): PlatformStatus {
  if (b.status === 'failed') return 'failed'
  if (b.status === 'done') return 'done'
  return 'running'
}

export function RunSidebar({
  bubbles,
  events,
  sharedState,
  isBlackbox,
  totalTokens,
  costUsd,
  durationSeconds,
}: {
  bubbles: Record<string, NodeBubbleState>
  events: RunEvent[]
  sharedState: Record<string, unknown>
  isBlackbox: boolean
  totalTokens: number
  costUsd: number
  durationSeconds: number
}) {
  const [tab, setTab] = useState<'graph' | 'shared_state' | 'timeline'>('graph')
  const nodes = Object.values(bubbles)

  return (
    <div className="flex flex-col gap-space-5">
      <div className="rounded-lg border border-border bg-surface p-space-4">
        <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
          <TabsList className="w-full">
            <TabsTrigger value="graph" className="flex-1">
              执行图
            </TabsTrigger>
            <TabsTrigger value="shared_state" className="flex-1">
              共享状态
            </TabsTrigger>
            <TabsTrigger value="timeline" className="flex-1">
              时间线
            </TabsTrigger>
          </TabsList>

          <TabsContent value="graph" className="mt-space-4">
            {isBlackbox ? (
              <BlackboxPlaceholder />
            ) : nodes.length === 0 ? (
              <p className="text-body-sm text-ink-500">尚无节点开始执行</p>
            ) : (
              <ul className="flex flex-col gap-space-2">
                {nodes.map((b) => (
                  <GraphNode key={b.node} bubble={b} />
                ))}
              </ul>
            )}
          </TabsContent>

          <TabsContent value="shared_state" className="mt-space-4">
            {Object.keys(sharedState).length === 0 ? (
              <p className="text-body-sm text-ink-500">
                {isBlackbox ? '该资源作者未声明可见输出字段' : '尚无共享状态数据'}
              </p>
            ) : (
              <pre className="max-h-80 overflow-auto rounded-xs bg-surface-muted p-space-3 font-mono text-body-sm text-ink-700">
                {JSON.stringify(sharedState, null, 2)}
              </pre>
            )}
          </TabsContent>

          <TabsContent value="timeline" className="mt-space-4">
            <ol className="flex max-h-96 flex-col gap-space-2 overflow-auto">
              {events
                .filter((e) => !isBlackbox || e.type !== 'node.thinking')
                .map((e) => (
                  <li key={e.id} className="text-body-sm flex items-center gap-space-2 text-ink-700">
                    <span className="text-caption font-mono text-ink-500">
                      {new Date(e.timestamp).toLocaleTimeString()}
                    </span>
                    <span className="font-mono">{e.type}</span>
                    {e.node && <span className="text-ink-500">· {e.node}</span>}
                  </li>
                ))}
            </ol>
          </TabsContent>
        </Tabs>
      </div>

      <div className="rounded-lg border border-border bg-surface p-space-5">
        <p className="text-headline-sm mb-space-4 text-ink-900">本次运行</p>
        <div className="grid grid-cols-3 gap-space-4">
          <Metric label="耗时" value={`${durationSeconds}s`} />
          <Metric label="Token" value={totalTokens.toLocaleString()} />
          <Metric label="成本" value={`$${costUsd.toFixed(4)}`} />
        </div>
      </div>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-space-1">
      <span className="text-data text-ink-900">{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

/**
 * Simplified stand-in for spec-14's full SVG execution graph — a
 * keyboard-focusable, expandable node list rather than a laid-out graph
 * with edges. It satisfies the same accessibility contract (Tab to
 * focus, Enter to expand a node's detail) and the timeline tab remains
 * the equivalent plain-text path either way; a real node-link diagram is
 * deferred to spec-17's Bundle editor, which already needs one.
 */
function GraphNode({ bubble }: { bubble: NodeBubbleState }) {
  const [expanded, setExpanded] = useState(false)
  const status = nodeStatus(bubble)

  return (
    <li className="rounded-xs border border-border bg-surface-page">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center justify-between px-space-3 py-space-2 text-left focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <span className="text-body-sm font-mono text-ink-900">{bubble.node}</span>
        <StatusChip status={status} />
      </button>
      {expanded && (
        <p className="text-body-sm border-t border-border px-space-3 py-space-2 text-ink-700">
          {bubble.status === 'failed' ? (bubble.errorText ?? '节点执行失败') : bubble.text || '（尚无产出）'}
        </p>
      )}
    </li>
  )
}

function BlackboxPlaceholder() {
  return (
    <div className="flex flex-col items-center gap-space-2 rounded-md border border-dashed border-border py-space-8 text-center">
      <Lock className="size-5 text-ink-500" aria-hidden />
      <p className="text-body-sm text-ink-500">黑盒节点</p>
    </div>
  )
}
