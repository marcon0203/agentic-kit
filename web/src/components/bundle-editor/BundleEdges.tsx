import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from '@xyflow/react'

import { cn } from '@/lib/utils'
import type { BundleEdge as BundleEdgeType } from '@/lib/bundleEditor/graphIO'

/** Regular edge — border-strong solid, condition/error label rendered via
 * EdgeLabelRenderer so it stays readable regardless of edge angle. */
export function BundleEdgeView({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  selected,
  data,
}: EdgeProps<BundleEdgeType>) {
  const [edgePath, labelX, labelY] = getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition })
  const invalid = data?.invalid

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          stroke: invalid ? 'var(--color-error)' : selected ? 'var(--color-primary)' : 'var(--color-border-strong)',
          strokeWidth: selected || invalid ? 2.5 : 1.5,
        }}
      />
      {(data?.condition || data?.parallel) && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            className="pointer-events-none absolute flex items-center gap-1"
          >
            {data?.parallel && (
              <span className="text-caption rounded-full bg-surface-muted px-1.5 py-0.5 text-ink-700">并行</span>
            )}
            {data?.condition && (
              <span
                className={cn(
                  'text-caption rounded-xs border bg-surface px-1.5 py-0.5 font-mono',
                  invalid ? 'border-[var(--color-error)] text-[var(--color-error)]' : 'border-border text-ink-700',
                )}
              >
                {data.condition}
              </span>
            )}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

/** Self-loop retry edge — warning dashed, per spec-17/spec-14's shared
 * visual language for a node's own retry branch. React Flow's default
 * bezier degenerates to a point when source===target, so this draws an
 * explicit small arc looping above the node instead. */
export function SelfLoopEdgeView({ id, sourceX, sourceY, selected, data }: EdgeProps<BundleEdgeType>) {
  const radius = 36
  const path = `M ${sourceX - 10} ${sourceY - 8} C ${sourceX - 10} ${sourceY - radius}, ${sourceX + 40} ${sourceY - radius}, ${sourceX + 30} ${sourceY - 8}`
  const invalid = data?.invalid

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        style={{
          stroke: invalid ? 'var(--color-error)' : 'var(--color-warning)',
          strokeWidth: selected || invalid ? 2.5 : 1.5,
          strokeDasharray: '3 3',
        }}
      />
      {data?.condition && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -100%) translate(${sourceX + 12}px, ${sourceY - radius - 4}px)` }}
            className="text-caption pointer-events-none absolute rounded-xs border border-[var(--color-warning)] bg-surface px-1.5 py-0.5 font-mono text-ink-700"
          >
            {data.condition}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}
