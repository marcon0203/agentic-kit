import { useState } from 'react'
import { Check, X } from 'lucide-react'

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
          <Check className="size-4 text-moss" aria-hidden />
        ) : (
          <X className="size-4 text-rust" aria-hidden />
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
      className="overflow-hidden rounded-lg border border-signal bg-signal-tint"
    >
      {/* The run has physically stopped here. The pulsing station is the
          same mark the hero rail uses, so "停在这一步" reads instantly. */}
      <div className="flex items-center gap-space-3 border-b border-signal/30 px-space-5 py-space-3">
        <span aria-hidden className="animate-gate-await size-2.5 shrink-0 rounded-full bg-signal" />
        <span className="text-label-md text-signal">运行已暂停，等你决定</span>
      </div>

      <div className="flex flex-col gap-space-4 px-space-5 py-space-4">
        <div className="flex flex-col gap-space-1">
          <p className="text-body-md text-ink-900">
            <code className="text-ref-lg">{gate.node}</code> 已经跑完了。
          </p>
          <p className="text-body-sm text-ink-700">
            通过就继续往下走；驳回会让这条分支带着你的理由停下，后面的节点不会执行。
          </p>
        </div>

        <div className="flex justify-end gap-space-3">
          {canApprove ? (
            <>
              {/* size="lg" (44px): an approval is a decision, and a decision
                  deserves a target you cannot miss by accident. */}
              <Button
                variant="outline"
                size="lg"
                disabled={pending !== null}
                onClick={() => act(false)}
              >
                {pending === 'reject' ? '驳回中…' : '驳回'}
              </Button>
              <Button size="lg" disabled={pending !== null} onClick={() => act(true)}>
                {pending === 'approve' ? '通过中…' : '通过'}
              </Button>
            </>
          ) : (
            <span className="text-body-sm text-ink-500">只有发起这次运行的人可以审批</span>
          )}
        </div>
      </div>
    </div>
  )
}
