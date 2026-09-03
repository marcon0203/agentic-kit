import { Link } from 'react-router-dom'
import { TriangleAlert } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { FieldError } from '@/lib/api/client'

/**
 * 从 depclosure 拼出来的 reason 文本里，把缺的那个依赖挖出来。
 *
 * 格式是后端 internal/depclosure/closure.go 自己拼的，形如
 * "依赖路径 bundle/web-app-builder@2.0 → agent/architect@1.0 未发布"——
 * 路径最后一段就是真正缺发布的那个资源，`kind/ref` 或 `kind/ref@version`，
 * kind 取值（bundle/agent/skill/mcp）与 ListingResourceType 完全一致。
 *
 * 只处理"未发布"这一种：路径不存在、循环依赖两种不是发布能解决的问题，
 * 硬凑一个目标只会把用户指错方向。
 */
function parseMissingDependency(reason: string): { type: string; ref: string; version?: string } | null {
  const match = reason.match(/^依赖路径 (.+) 未发布$/)
  if (!match) return null
  const last = match[1].split(' → ').pop()
  if (!last) return null
  const seg = last.match(/^(bundle|agent|skill|mcp)\/(.+?)(?:@([^@]+))?$/)
  if (!seg) return null
  const [, type, ref, version] = seg
  return { type, ref, version }
}

/**
 * spec-16's "发布失败的依赖引导" — a single generic error line is a bad
 * experience when there can be several missing dependencies at once, so
 * every one from `details[]` gets its own row with the backend's own
 * full dependency-path text (spec's format already reads
 * "web-app-builder → architect@1.0 → mcp/internal-search@1.0 未发布"),
 * a same-content-preserving "去发布" link (a new tab, so the half-filled
 * publish form here is never lost), and a single "重新校验" action that
 * just resubmits the current form in place.
 *
 * "去发布" used to point at `/apps/bundles`, which is just the Bundle
 * list — no publish form lives there at all, so the button landed the
 * user on a page with nothing to click. It now goes to `/apps/publish`
 * (the real publish form), with the missing resource's type/ref/version
 * parsed out of the reason text and carried as query params so the form
 * opens pre-filled instead of asking the user to retype what the error
 * message already told them.
 */
export function DependencyErrorPanel({ errors, onRevalidate }: { errors: FieldError[]; onRevalidate: () => void }) {
  return (
    <div className="rounded-sm border border-signal bg-signal-tint p-space-5">
      <div className="flex items-center gap-space-2">
        <TriangleAlert className="size-4 text-signal" aria-hidden />
        <p className="text-label-md text-ink-900">存在 {errors.length} 个未满足的发布依赖，无法发布</p>
      </div>
      <ol className="mt-space-4 flex flex-col gap-space-4">
        {errors.map((e, i) => {
          const target = parseMissingDependency(e.reason)
          const to = target
            ? `/apps/publish?${new URLSearchParams({
                type: target.type,
                ref: target.ref,
                ...(target.version ? { version: target.version } : {}),
              })}`
            : '/apps/publish'
          return (
            <li key={i} className="flex items-start justify-between gap-space-4 border-t border-border pt-space-3 first:border-t-0 first:pt-0">
              <div>
                <p className="text-ref text-ink-500">{e.field}</p>
                <p className="text-body-sm text-ink-700">{e.reason}</p>
              </div>
              <Button asChild variant="outline" size="sm" className="shrink-0">
                <Link to={to} target="_blank" rel="noopener noreferrer">
                  去发布
                </Link>
              </Button>
            </li>
          )
        })}
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
