import type { ReactNode } from 'react'

/**
 * design-system.md: every empty state must give a reason AND a next
 * action — "还没有任何资源，注册一个" reads very differently from "没有
 * 找到匹配结果，清除筛选" and the two must never share copy.
 */
export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-space-3 rounded-lg border border-dashed border-border py-space-11 text-center">
      <p className="text-label-md text-ink-900">{title}</p>
      <p className="text-body-sm max-w-[420px] text-ink-500">{description}</p>
      {action}
    </div>
  )
}

export function ListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-space-2">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="h-14 animate-pulse rounded-md bg-surface-muted" />
      ))}
    </div>
  )
}

export function ErrorPanel({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex items-center justify-between rounded-md border border-[var(--color-error)] px-space-4 py-space-3">
      <p className="text-body-sm text-[var(--color-error)]">{message}</p>
      <button type="button" onClick={onRetry} className="text-body-sm font-medium text-primary hover:underline">
        重试
      </button>
    </div>
  )
}
