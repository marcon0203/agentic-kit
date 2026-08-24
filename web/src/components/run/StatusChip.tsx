import { Check, CircleDashed, Lock, TriangleAlert, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export type PlatformStatus = 'pending' | 'running' | 'gate' | 'done' | 'failed' | 'blackbox'

const STATUS_META: Record<PlatformStatus, { label: string; icon: typeof Check; color: string }> = {
  pending: { label: '等待中', icon: CircleDashed, color: 'text-status-pending' },
  running: { label: '运行中', icon: CircleDashed, color: 'text-status-running' },
  gate: { label: '待审批', icon: TriangleAlert, color: 'text-status-gate' },
  done: { label: '已完成', icon: Check, color: 'text-status-done' },
  failed: { label: '失败', icon: X, color: 'text-status-failed' },
  blackbox: { label: '黑盒', icon: Lock, color: 'text-status-blackbox' },
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
        'text-caption inline-flex items-center gap-1.5',
        meta.color,
        // A gate is the only status that pulses — it is waiting on the
        // person reading it. "Running" is not waiting on anybody.
        status === 'gate' && 'animate-gate-await rounded-full',
        className,
      )}
    >
      <Icon className="size-3" aria-hidden />
      {meta.label}
    </span>
  )
}
