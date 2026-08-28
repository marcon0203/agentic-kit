import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * The run rail — this platform's signature element.
 *
 * Everything here is one idea: work moves along a track, and it stops at a
 * gate until a person answers. That is the product, so it is drawn rather
 * than described, and it is the only place in the app that animates on its
 * own. Every other surface stays quiet so this one reads.
 *
 * The same vocabulary is reused for the empty states: an empty rail says
 * "nothing has come through here yet" far more directly than a dashed box
 * around the words "no data".
 */

export type StationState = 'done' | 'running' | 'gate' | 'pending' | 'failed'

const STATION_RING: Record<StationState, string> = {
  done: 'border-moss bg-moss',
  running: 'border-blueprint bg-blueprint',
  gate: 'border-signal bg-signal',
  pending: 'border-border-strong bg-surface',
  failed: 'border-rust bg-rust',
}

const SEGMENT_COLOR: Record<StationState, string> = {
  done: 'bg-moss',
  running: 'bg-blueprint',
  gate: 'bg-signal',
  pending: 'bg-border',
  failed: 'bg-rust',
}

/**
 * A single station on the rail. A gate pulses because it is literally
 * waiting for the person looking at it; nothing else does.
 */
export function Station({
  state,
  label,
  sublabel,
  className,
}: {
  state: StationState
  label: string
  sublabel?: string
  className?: string
}) {
  return (
    <div className={cn('flex min-w-0 flex-col items-center gap-space-2', className)}>
      <span
        aria-hidden
        className={cn(
          'size-3 shrink-0 rounded-full border-2',
          STATION_RING[state],
          state === 'gate' && 'animate-gate-await',
        )}
      />
      <span className="flex min-w-0 flex-col items-center gap-0.5">
        <span
          className={cn(
            'text-ref max-w-[13ch] truncate',
            state === 'pending' ? 'text-ink-500' : 'text-ink-900',
          )}
        >
          {label}
        </span>
        {sublabel ? <span className="text-caption text-ink-500">{sublabel}</span> : null}
      </span>
    </div>
  )
}

/** The connecting track between two stations. */
export function Segment({ state, className }: { state: StationState; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        'mt-[5px] h-px min-w-space-6 flex-1 origin-left',
        SEGMENT_COLOR[state],
        state === 'pending' && 'opacity-70',
        className,
      )}
    />
  )
}

export type RailNode = {
  id: string
  label: string
  sublabel?: string
  state: StationState
}

/**
 * A horizontal rail of stations. `nodes` is read in order — the segment
 * before a station takes that station's state, so the track is coloured up
 * to wherever the work has actually reached.
 */
export function Rail({ nodes, className }: { nodes: RailNode[]; className?: string }) {
  return (
    <div className={cn('flex items-start', className)}>
      {nodes.map((node, i) => (
        <div key={node.id} className="flex min-w-0 flex-1 items-start last:flex-none">
          {i > 0 ? <Segment state={node.state === 'pending' ? 'pending' : node.state} /> : null}
          <Station state={node.state} label={node.label} sublabel={node.sublabel} />
        </div>
      ))}
    </div>
  )
}

/**
 * An empty state drawn as a rail nothing has travelled yet.
 *
 * The copy rule (design-system.md): say what would appear here and give the
 * one action that makes it appear. Never "no data" — that tells the reader
 * something they can already see.
 */
export function EmptyRail({
  title,
  description,
  action,
  className,
}: {
  title: string
  description: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center gap-space-4 px-space-6 py-space-10 text-center',
        className,
      )}
    >
      <div aria-hidden className="flex w-full max-w-[280px] items-center">
        <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
        <span className="h-px flex-1 bg-border" />
        <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
        <span className="h-px flex-1 bg-border" />
        <span className="size-2.5 shrink-0 rounded-full border-2 border-border-strong bg-surface" />
      </div>
      <div className="flex flex-col gap-space-2">
        <p className="text-display-sm text-ink-900">{title}</p>
        <p className="text-body-sm mx-auto max-w-[46ch] text-ink-700">{description}</p>
      </div>
      {action}
    </div>
  )
}
