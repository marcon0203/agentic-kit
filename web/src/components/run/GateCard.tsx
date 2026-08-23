import { useState } from 'react'
import { Check, TriangleAlert, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { TimelineEntry } from '@/lib/runs/timeline'

type GateEntry = Extract<TimelineEntry, { kind: 'gate' }>

/**
 * ds-gate-card — inline in the conversation, never a modal (design-system
 * §八: approving needs the surrounding context a modal would hide).
 * Action buttons render only for `canApprove` — the run's own
 * triggered_by user (spec-11's V1 approver rule) — everyone else sees
 * "等待他人审批"; the real enforcement is the backend's 50004, this is
 * only UX.
 */
export function GateCard({
  gate,
  canApprove,
  onResolve,
}: {
  gate: GateEntry
  canApprove: boolean
  onResolve: (node: string, approved: boolean) => Promise<void>
}) {
  const [pending, setPending] = useState<'approve' | 'reject' | null>(null)

  if (gate.status !== 'pending') {
    const done = gate.status === 'approved'
    const label = done ? '已通过' : gate.status === 'timeout' ? '已超时处理' : '已驳回'
    return (
      <div className="text-body-sm flex items-center gap-space-2 rounded-md border border-border bg-surface px-space-4 py-space-3 text-ink-700">
        {done ? (
          <Check className="size-4 text-[var(--color-success)]" aria-hidden />
        ) : (
          <X className="size-4 text-[var(--color-error)]" aria-hidden />
        )}
        <span>
          {gate.node} 的审批{label}
        </span>
      </div>
    )
  }

  async function act(approved: boolean) {
    setPending(approved ? 'approve' : 'reject')
    try {
      await onResolve(gate.node, approved)
    } finally {
      setPending(null)
    }
  }

  return (
    <div
      role="status"
      aria-live="assertive"
      className="rounded-md border border-[var(--color-warning)] p-space-5"
      style={{ backgroundColor: 'color-mix(in srgb, var(--color-warning) 8%, var(--color-surface))' }}
    >
      <div className="flex items-start gap-space-3">
        <TriangleAlert className="mt-0.5 size-5 shrink-0 text-[var(--color-warning)]" aria-hidden />
        <div className="flex-1">
          <p className="text-label-md text-ink-900">{gate.node} 已完成，需要你确认后继续</p>
          <p className="text-body-sm mt-space-1 text-ink-700">请查看上方产出，决定是否继续执行后续步骤。</p>

          <div className="mt-space-4 flex justify-end gap-space-3">
            {canApprove ? (
              <>
                {/* size="lg" (44px) — design-system.md's explicit
                    ≥40×40px requirement for approval actions. */}
                <Button variant="secondary" size="lg" disabled={pending !== null} onClick={() => act(false)}>
                  {pending === 'reject' ? '处理中…' : '驳回'}
                </Button>
                <Button size="lg" disabled={pending !== null} onClick={() => act(true)}>
                  {pending === 'approve' ? '处理中…' : '通过'}
                </Button>
              </>
            ) : (
              <span className="text-body-sm text-ink-500">等待他人审批</span>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
