import { Check, CircleDashed, Lock, TriangleAlert, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export type PlatformStatus = 'pending' | 'running' | 'gate' | 'done' | 'failed' | 'blackbox'

/* A status is a tinted chip: colour + icon + text, all three. The tint makes
   status scannable down a table column at a glance; the deep text stops keep
   caption-size text above 4.5:1 on its own tint (the vivid hues fail there).
   A gate is the only status that pulses — it waits on the person reading it. */
const STATUS_META: Record<PlatformStatus, { label: string; icon: typeof Check; chip: string }> = {
  pending: { label: '等待中', icon: CircleDashed, chip: 'bg-surface-muted text-ink-700' },
  running: { label: '运行中', icon: CircleDashed, chip: 'bg-blueprint-tint text-violet' },
  gate: { label: '待审批', icon: TriangleAlert, chip: 'bg-signal-tint text-signal-deep' },
  done: { label: '已完成', icon: Check, chip: 'bg-moss-tint text-moss-deep' },
  failed: { label: '失败', icon: X, chip: 'bg-rust-tint text-rust-deep' },
  blackbox: { label: '黑盒', icon: Lock, chip: 'bg-surface-muted text-ink-700' },
}

/**
 * design-system.md §1.2: every platform status pairs its color with an
 * icon + text — never color alone. Used for node/run/gate status
 * everywhere in the Chat page.
 */
export function StatusChip({ status, className }: { status: PlatformStatus; className?: string }) {
  const meta = STATUS_META[status]
  const Icon = meta.icon
  return (
    <span
      className={cn(
        'text-caption inline-flex w-max items-center gap-1 rounded-full px-space-2 py-0.5',
        meta.chip,
        // A gate is the only status that pulses — it is waiting on the
        // person reading it. "Running" is not waiting on anybody.
        status === 'gate' && 'animate-gate-await',
        className,
      )}
    >
      <Icon className="size-3" aria-hidden />
      {meta.label}
    </span>
  )
}
