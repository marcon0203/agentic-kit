import { Handle, Position, type NodeProps } from '@xyflow/react'
import { TriangleAlert, CircleAlert } from 'lucide-react'

import { cn } from '@/lib/utils'
import type { AgentNode as AgentNodeType } from '@/lib/bundleEditor/graphIO'

/**
 * ds-graph-node, editing variant (spec-17): unlike the read-only execution
 * graph (spec-14), nodes here never carry a run-status color — only
 * selection (primary ring) and validation (error ring + icon) states.
 * human gate is a warning-colored corner badge + icon, never color alone.
 */
export function AgentNode({ data, selected }: NodeProps<AgentNodeType>) {
  return (
    <div
      className={cn(
        'relative w-[180px] rounded-sm border bg-surface px-space-4 py-space-3 shadow-sm transition-shadow',
        selected ? 'border-blueprint ring-2 ring-blueprint/25' : 'border-border-strong',
        data.invalid && 'border-rust ring-2 ring-rust/20',
      )}
    >
      <Handle type="target" position={Position.Left} className="!bg-border-strong" />
      <Handle type="source" position={Position.Right} className="!bg-border-strong" />

      {data.gate && (
        <span
          title={`human gate：超时 ${data.gate.timeout_seconds ?? '未设置'}s，策略 ${data.gate.on_timeout}`}
          className="absolute -right-2 -top-2 flex items-center gap-0.5 rounded-full bg-signal px-1.5 py-0.5 text-white shadow-sm"
        >
          <TriangleAlert className="size-3" aria-hidden />
        </span>
      )}
      {data.invalid && (
        <span className="absolute -left-2 -top-2 flex items-center justify-center rounded-full bg-rust p-0.5 text-white shadow-sm">
          <CircleAlert className="size-3" aria-hidden />
        </span>
      )}

      {/* A node's name is an identifier the DSL will contain verbatim, so
          it is set in the mono face like every other ref in the app. */}
      <p className="text-ref truncate text-ink-900">{data.alias || data.ref}</p>
      {data.alias && <p className="text-caption truncate text-ink-500">{data.ref}</p>}
    </div>
  )
}

export function EndNode({ selected }: NodeProps) {
  return (
    <div
      className={cn(
        'text-ref flex w-[100px] items-center justify-center rounded-full border bg-surface-muted px-space-4 py-space-3 text-ink-700 shadow-sm',
        selected ? 'border-blueprint ring-2 ring-blueprint/25' : 'border-border-strong',
      )}
    >
      <Handle type="target" position={Position.Left} className="!bg-border-strong" />
      END
    </div>
  )
}
