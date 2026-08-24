import type { ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'
import { EmptyRail } from '@/components/common/Rail'

/**
 * Every empty state gives a reason AND a next action.
 *
 * "还没有任何资源，注册一个" and "没有找到匹配结果，清除筛选" are two
 * different situations and must never share copy — the first is an
 * invitation, the second is a dead end the reader needs help out of.
 *
 * The drawing is a rail nothing has travelled yet (see Rail.tsx), which says
 * "this is where things will appear" in the platform's own vocabulary. A
 * dashed rectangle says only "box".
 */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: ReactNode
}) {
  return <EmptyRail title={title} description={description} action={action} />
}

export function ListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="overflow-hidden rounded-lg border border-border" aria-hidden>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="border-b border-border bg-surface px-space-5 py-space-4 last:border-0">
          <div className="h-3 w-1/3 animate-pulse rounded-xs bg-surface-muted" />
        </div>
      ))}
    </div>
  )
}

/**
 * An error tells the reader what failed and offers the retry — it does not
 * apologise, and it never says "出错了" without saying what.
 */
export function ErrorPanel({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div
      role="alert"
      className="flex items-center justify-between gap-space-4 rounded-lg border border-rust bg-rust-tint px-space-5 py-space-3"
    >
      <p className="text-body-sm flex items-center gap-space-2 text-rust">
        <AlertTriangle className="size-4 shrink-0" aria-hidden />
        {message}
      </p>
      <button
        type="button"
        onClick={onRetry}
        className="text-body-sm shrink-0 font-medium text-rust underline underline-offset-2 hover:no-underline"
      >
        重试
      </button>
    </div>
  )
}
