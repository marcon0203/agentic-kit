import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, Check } from 'lucide-react'

import { apiClient, unwrap } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type InstalledPluginTool = components['schemas']['InstalledPluginTool']

/**
 * capabilities.tools[] 能力选择器的插件半边——GET /plugins/installed/tools
 * 把每个已安装插件的 manifest.extensions.tools[] 和 renderers[] 都解析成
 * "plugin:{plugin_id}/{name}" 这样的 ref，和 ResourceMultiSelect 挑的资源
 * 中心 ref 写进同一个 tools 数组（spec-20 §5.1："不新增字段"）。renderers[]
 * 必须一起列出来：一个纯渲染器插件（比如图表渲染，没有任何模型可调用的
 * 工具）只有它的 ref 被勾进这里，auto_render 才会真的生效——不是装了就自动
 * 生效，这一步不能省。
 */
export function PluginToolMultiSelect({
  selected,
  onChange,
  variant = 'pill',
}: {
  selected: string[]
  onChange: (refs: string[]) => void
  variant?: 'pill' | 'card'
}) {
  const query = useQuery({
    queryKey: ['plugins', 'installed', 'tools'],
    queryFn: async () => unwrap<{ items: InstalledPluginTool[] }>(await apiClient.GET('/plugins/installed/tools', {})),
  })

  const options = query.data?.items ?? []

  // 按插件分组，供 card 变体展开渲染。map 保序——options 本来就是按
  // 插件连续排列的（ListInstalledTools 按安装顺序遍历）。
  const grouped = useMemo(() => {
    const map = new Map<string, { displayName: string; tools: InstalledPluginTool[] }>()
    for (const opt of options) {
      const g = map.get(opt.plugin_id) ?? {
        displayName: opt.plugin_display_name || opt.plugin_id,
        tools: [],
      }
      g.tools.push(opt)
      map.set(opt.plugin_id, g)
    }
    return Array.from(map.values())
  }, [options])

  function toggle(ref: string) {
    onChange(selected.includes(ref) ? selected.filter((r) => r !== ref) : [...selected, ref])
  }

  if (query.isLoading) {
    return <p className="text-body-sm text-ink-500">加载已安装插件…</p>
  }
  if (options.length === 0) {
    return (
      <p className="text-body-sm text-ink-500">
        还没有安装任何插件，前往{' '}
        <a href="/apps/tool?tab=plugin" className="text-blueprint underline">
          组件广场 · 插件
        </a>{' '}
        安装。
      </p>
    )
  }

  // 已失效的引用：selected 里有 plugin: 前缀但不在当前已安装版本的工具列表
  // 里——通常是插件升级后重命名/删除了某个工具，旧 ref 残留在 agent 定义里。
  // 当前版本的工具列表里看不到它们，用户没法取消勾选，这里单独列出来可移除。
  const staleRefs = selected.filter(
    (ref) => ref.startsWith('plugin:') && !options.some((opt) => opt.ref === ref),
  )

  if (variant === 'card') {
    return (
      <div className="flex flex-col gap-space-3">
        {staleRefs.length > 0 && (
          <div className="flex flex-col gap-space-2 rounded-lg border border-signal/30 bg-signal-tint/40 p-space-3">
            <span className="text-caption text-ink-500">
              已失效的工具（插件升级后不再提供，建议移除）
            </span>
            <div className="flex flex-wrap gap-space-2">
              {staleRefs.map((ref) => (
                <button
                  key={ref}
                  type="button"
                  onClick={() => toggle(ref)}
                  className="text-caption inline-flex items-center gap-1 rounded-full border border-signal/40 bg-surface px-space-2 py-0.5 text-rust hover:bg-surface-muted"
                  title="点击移除"
                >
                  {ref.replace('plugin:', '')}
                  <span aria-hidden>×</span>
                </button>
              ))}
            </div>
          </div>
        )}
        {grouped.map((group) => {
          const selectedCount = group.tools.filter((t) => selected.includes(t.ref)).length
          const anySelected = selectedCount > 0
          return (
            <details
              key={group.displayName}
              open={anySelected}
              className="group rounded-lg border border-border bg-surface"
            >
              <summary className="flex cursor-pointer list-none items-center justify-between gap-space-3 px-space-4 py-space-3 transition-colors hover:bg-surface-muted">
                <span className="flex items-center gap-space-2">
                  <span className="text-body-md font-medium text-ink-900">{group.displayName}</span>
                  <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">
                    {group.tools.length} 个工具
                  </span>
                </span>
                <span className="flex items-center gap-space-2">
                  {anySelected && (
                    <span className="text-caption tabular text-blueprint">已选 {selectedCount}</span>
                  )}
                  <ChevronDown
                    className="size-4 text-ink-500 transition-transform group-open:rotate-180"
                    aria-hidden
                  />
                </span>
              </summary>
              <div className="flex flex-col divide-y divide-border border-t border-border">
                {group.tools.map((tool) => {
                  const active = selected.includes(tool.ref)
                  return (
                    <label
                      key={tool.ref}
                      className={cn(
                        'flex cursor-pointer items-start gap-space-3 px-space-4 py-space-3 transition-colors hover:bg-surface-muted',
                        active && 'bg-blueprint-tint/40',
                      )}
                    >
                      <input
                        type="checkbox"
                        className="sr-only"
                        checked={active}
                        onChange={() => toggle(tool.ref)}
                      />
                      <span
                        className={cn(
                          'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border transition-colors',
                          active
                            ? 'border-primary bg-primary text-white'
                            : 'border-border bg-surface',
                        )}
                      >
                        {active && <Check className="size-3" aria-hidden />}
                      </span>
                      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="text-body-sm font-medium text-ink-900">{tool.tool_name}</span>
                        {tool.description && (
                          <span className="text-caption text-ink-500">{tool.description}</span>
                        )}
                      </span>
                    </label>
                  )
                })}
              </div>
            </details>
          )
        })}
      </div>
    )
  }

  return (
    <div className="flex flex-wrap gap-space-2">
      {options.map((opt) => {
        const active = selected.includes(opt.ref)
        return (
          <button
            key={opt.ref}
            type="button"
            title={opt.description}
            onClick={() => toggle(opt.ref)}
            className={cn(
              'text-body-sm flex items-center gap-space-2 rounded-full border px-space-3 py-space-2',
              active ? 'border-primary bg-blueprint-tint text-blueprint' : 'border-border bg-surface text-ink-700 hover:border-border-strong',
            )}
          >
            {opt.tool_name}
            <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">{opt.plugin_id}</span>
          </button>
        )
      })}
      {staleRefs.map((ref) => (
        <button
          key={ref}
          type="button"
          title="插件升级后已失效，点击移除"
          onClick={() => toggle(ref)}
          className="text-caption inline-flex items-center gap-1 rounded-full border border-signal/40 bg-surface px-space-2 py-0.5 text-rust hover:bg-surface-muted"
        >
          {ref.replace('plugin:', '')}
          <span aria-hidden>×</span>
        </button>
      ))}
    </div>
  )
}
