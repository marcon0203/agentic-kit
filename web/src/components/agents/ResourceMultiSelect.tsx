import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { cn } from '@/lib/utils'
import { CheckCardGroup } from './CheckCardGroup'

type Resource = components['schemas']['Resource']
type ResourceType = components['schemas']['ResourceType']

// "tool"-kind resources can be a plain HTTP tool, an MCP-backed toolset
// (kind "mcp", not this), or a sandbox component — config.component_type
// is how the compiler tells them apart (internal/orchestrator/adk's
// compileTools). Options here look identical otherwise, so a sandbox
// resource needs a visible tag or there's no way to tell it apart from a
// plain tool while picking an Agent's capabilities.
const COMPONENT_TYPE_LABEL: Record<string, string> = {
  sandbox: '沙箱',
}

/**
 * 能力白名单 selector — pulls from the resource center, greys out
 * disabled resources with a hover tooltip explaining why (spec-15).
 */
export function ResourceMultiSelect({
  types,
  selected,
  onChange,
  variant = 'pill',
}: {
  types: ResourceType[]
  selected: string[]
  onChange: (refs: string[]) => void
  variant?: 'pill' | 'card'
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

  if (variant === 'card') {
    return (
      <CheckCardGroup
        options={options.map((opt) => {
          const disabled = opt.status !== 1
          const componentType = (opt.config as { component_type?: string } | undefined)?.component_type
          const componentLabel = componentType ? COMPONENT_TYPE_LABEL[componentType] : undefined
          return {
            value: opt.ref,
            label: (
              <span className="flex items-center gap-space-2">
                {opt.ref}
                {componentLabel && (
                  <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">
                    {componentLabel}
                  </span>
                )}
              </span>
            ),
            disabled,
            disabledReason: '该资源已被停用，无法在新的能力白名单中选用',
          }
        })}
        value={selected}
        onChange={onChange}
      />
    )
  }

  return (
    <div className="flex flex-wrap gap-space-2">
      {options.map((opt) => {
        const disabled = opt.status !== 1
        const active = selected.includes(opt.ref)
        const componentType = (opt.config as { component_type?: string } | undefined)?.component_type
        const componentLabel = componentType ? COMPONENT_TYPE_LABEL[componentType] : undefined
        return (
          <button
            key={opt.id}
            type="button"
            disabled={disabled}
            title={disabled ? '该资源已被停用，无法在新的能力白名单中选用' : undefined}
            onClick={() => toggle(opt.ref)}
            className={cn(
              'text-body-sm flex items-center gap-space-2 rounded-full border px-space-3 py-space-2',
              disabled && 'cursor-not-allowed border-border bg-surface-muted text-ink-500 opacity-60',
              !disabled && active && 'border-primary bg-blueprint-tint text-blueprint',
              !disabled && !active && 'border-border bg-surface text-ink-700 hover:border-border-strong',
            )}
          >
            {opt.ref}
            {componentLabel && (
              <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">
                {componentLabel}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
