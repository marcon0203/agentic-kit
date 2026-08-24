import { useState } from 'react'
import { Lock } from 'lucide-react'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Figure, Ref } from '@/components/common/Page'
import { cn } from '@/lib/utils'
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
              <p className="text-body-sm text-ink-500">还没有节点开始执行。</p>
            ) : (
              /* The rail again, running downwards: each node is a station
                 and the track between them shows how far the run got. */
              <ul className="flex flex-col">
                {nodes.map((b, i) => (
                  <GraphNode key={b.node} bubble={b} isLast={i === nodes.length - 1} />
                ))}
              </ul>
            )}
          </TabsContent>

          <TabsContent value="shared_state" className="mt-space-4">
            {Object.keys(sharedState).length === 0 ? (
              <p className="text-body-sm text-ink-500">
                {isBlackbox
                  ? '作者没有声明对外可见的输出字段，所以这里是空的。'
                  : '还没有节点往共享状态里写过东西。'}
              </p>
            ) : (
              <pre className="text-ref max-h-80 overflow-auto rounded-sm border border-border bg-surface-muted p-space-3 text-ink-700">
                {JSON.stringify(sharedState, null, 2)}
              </pre>
            )}
          </TabsContent>

          <TabsContent value="timeline" className="mt-space-4">
            <ol className="flex max-h-96 flex-col gap-space-2 overflow-auto">
              {events
                .filter((e) => !isBlackbox || e.type !== 'node.thinking')
                .map((e) => (
                  <li key={e.id} className="flex items-baseline gap-space-2">
                    <span className="text-ref tabular shrink-0 text-ink-500">
                      {new Date(e.timestamp).toLocaleTimeString()}
                    </span>
                    <span className="text-ref min-w-0 truncate text-ink-900">{e.type}</span>
                    {e.node && <span className="text-caption shrink-0 text-ink-500">{e.node}</span>}
                  </li>
                ))}
            </ol>
          </TabsContent>
        </Tabs>
      </div>

      <div className="rounded-lg border border-border bg-surface p-space-5">
        <p className="text-display-sm mb-space-4 border-b border-border pb-space-2 text-ink-900">
          这次运行花了
        </p>
        <div className="grid grid-cols-3 gap-space-4">
          <Figure value={`${durationSeconds}s`} label="耗时" />
          <Figure value={totalTokens.toLocaleString()} label="Token" />
          <Figure value={`$${costUsd.toFixed(4)}`} label="成本" />
        </div>
      </div>
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
function GraphNode({ bubble, isLast }: { bubble: NodeBubbleState; isLast: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const status = nodeStatus(bubble)

  const dot =
    status === 'failed' ? 'bg-rust' : status === 'done' ? 'bg-moss' : 'bg-blueprint'

  return (
    <li className="flex gap-space-3">
      {/* The vertical track. It continues past every node but the last, so
          the column reads as one run rather than a stack of chips. */}
      <span aria-hidden className="flex flex-col items-center pt-[9px]">
        <span className={cn('size-2.5 shrink-0 rounded-full', dot)} />
        {!isLast && <span className="w-px flex-1 bg-border" />}
      </span>

      <div className={cn('min-w-0 flex-1', !isLast && 'pb-space-3')}>
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
          className="flex w-full items-center justify-between gap-space-2 rounded-xs py-0.5 text-left"
        >
          <Ref>{bubble.node}</Ref>
          <StatusChip status={status} />
        </button>
        {expanded && (
          <p className="text-body-sm mt-space-2 rounded-sm bg-surface-muted px-space-3 py-space-2 text-ink-700">
            {bubble.status === 'failed'
              ? (bubble.errorText ?? '这个节点执行失败了，没有更多信息。')
              : bubble.text || '这个节点还没有产出内容。'}
          </p>
        )}
      </div>
    </li>
  )
}

function BlackboxPlaceholder() {
  return (
    <div className="flex flex-col items-center gap-space-2 rounded-sm border border-border bg-surface-muted px-space-4 py-space-8 text-center">
      <Lock className="size-5 text-ink-500" aria-hidden />
      <p className="text-body-sm text-ink-700">这是别人发布的资源</p>
      <p className="text-caption max-w-[32ch] text-ink-500">
        你能看到它的输出，但看不到内部有哪些节点、怎么连的。
      </p>
    </div>
  )
}
