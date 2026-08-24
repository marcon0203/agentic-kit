import { Link } from 'react-router-dom'
import { TriangleAlert } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { FieldError } from '@/lib/api/client'

/**
 * spec-16's "发布失败的依赖引导" — a single generic error line is a bad
 * experience when there can be several missing dependencies at once, so
 * every one from `details[]` gets its own row with the backend's own
 * full dependency-path text (spec's format already reads
 * "web-app-builder → architect@1.0 → mcp/internal-search@1.0 未发布"),
 * a same-content-preserving "去发布" link (a new tab, so the half-filled
 * publish form here is never lost), and a single "重新校验" action that
 * just resubmits the current form in place.
 */
export function DependencyErrorPanel({ errors, onRevalidate }: { errors: FieldError[]; onRevalidate: () => void }) {
  return (
    <div className="rounded-sm border border-signal bg-signal-tint p-space-5">
      <div className="flex items-center gap-space-2">
        <TriangleAlert className="size-4 text-signal" aria-hidden />
        <p className="text-label-md text-ink-900">存在 {errors.length} 个未满足的发布依赖，无法发布</p>
      </div>
      <ol className="mt-space-4 flex flex-col gap-space-4">
        {errors.map((e, i) => (
          <li key={i} className="flex items-start justify-between gap-space-4 border-t border-border pt-space-3 first:border-t-0 first:pt-0">
            <div>
              <p className="text-ref text-ink-500">{e.field}</p>
              <p className="text-body-sm text-ink-700">{e.reason}</p>
            </div>
            <Button asChild variant="outline" size="sm" className="shrink-0">
              <Link to="/apps?tab=manage" target="_blank" rel="noopener noreferrer">
                去发布
              </Link>
            </Button>
          </li>
        ))}
      </ol>
      <div className="mt-space-5 flex items-center justify-between">
        <p className="text-caption text-ink-500">全部发布后再回来重试</p>
        <Button variant="outline" size="sm" onClick={onRevalidate}>
          重新校验
        </Button>
      </div>
    </div>
  )
}
