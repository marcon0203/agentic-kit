import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { cn } from '@/lib/utils'

type Resource = components['schemas']['Resource']
type ResourceType = components['schemas']['ResourceType']

/**
 * 能力白名单 selector — pulls from the resource center, greys out
 * disabled resources with a hover tooltip explaining why (spec-15).
 */
export function ResourceMultiSelect({
  types,
  selected,
  onChange,
}: {
  types: ResourceType[]
  selected: string[]
  onChange: (refs: string[]) => void
}) {
  const query = useQuery({
    queryKey: ['resource-picker', types],
    queryFn: async () => {
      const responses = await Promise.all(
        types.map((type) => apiClient.GET('/resources', { params: { query: { type } } })),
      )
      const lists = responses.map((res) => unwrap<{ items: Resource[] }>(res))
      return lists.flatMap((l) => l.items)
    },
  })

  const options = query.data ?? []

  function toggle(ref: string) {
    onChange(selected.includes(ref) ? selected.filter((r) => r !== ref) : [...selected, ref])
  }

  if (query.isLoading) {
    return <p className="text-body-sm text-ink-500">加载资源列表…</p>
  }
  if (options.length === 0) {
    return <p className="text-body-sm text-ink-500">资源中心暂无可选资源，请先前往注册。</p>
  }

  return (
    <div className="flex flex-wrap gap-space-2">
      {options.map((opt) => {
        const disabled = opt.status !== 1
        const active = selected.includes(opt.ref)
        return (
          <button
            key={opt.id}
            type="button"
            disabled={disabled}
            title={disabled ? '该资源已被停用，无法在新的能力白名单中选用' : undefined}
            onClick={() => toggle(opt.ref)}
            className={cn(
              'text-body-sm rounded-full border px-space-3 py-space-2',
              disabled && 'cursor-not-allowed border-border bg-surface-muted text-ink-500 opacity-60',
              !disabled && active && 'border-primary bg-blueprint-tint text-blueprint',
              !disabled && !active && 'border-border bg-surface text-ink-700 hover:border-border-strong',
            )}
          >
            {opt.ref}
          </button>
        )
      })}
    </div>
  )
}
