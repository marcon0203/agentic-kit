import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type InstalledPluginTool = components['schemas']['InstalledPluginTool']

/**
 * capabilities.tools[] 能力选择器的插件半边——GET /plugins/installed/tools
 * 把每个已安装插件的 manifest.extensions.tools[] 解析成
 * "plugin:{plugin_id}/{tool_name}" 这样的 ref，和 ResourceMultiSelect 挑的
 * 资源中心 ref 写进同一个 tools 数组（spec-20 §5.1："不新增字段"）。装了
 * 插件却在这里找不到入口是不给用，所以这个选择器必须和 ResourceMultiSelect
 * 并排放在同一个"tools"字段下，而不是单独一个"插件"步骤。
 */
export function PluginToolMultiSelect({ selected, onChange }: { selected: string[]; onChange: (refs: string[]) => void }) {
  const query = useQuery({
    queryKey: ['plugins', 'installed', 'tools'],
    queryFn: async () => unwrap<{ items: InstalledPluginTool[] }>(await apiClient.GET('/plugins/installed/tools', {})),
  })

  const options = query.data?.items ?? []

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
    </div>
  )
}
