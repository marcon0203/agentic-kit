import * as React from 'react'
import { ChevronDownIcon, X } from 'lucide-react'

import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { ProviderIcon } from '@/components/models/ProviderIcon'
import type { components } from '@/lib/api/schema'

type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']

interface FallbackMultiSelectProps {
  catalog: ModelCatalogEntry[]
  value: string
  onChange: (v: string) => void
  /** 主模型的 provider/model；它自己不该出现在降级链里 */
  exclude?: string
}

interface ProviderGroup {
  provider: string
  label: string
  items: ModelCatalogEntry[]
}

/**
 * Fallback 模型多选：**先选供应商，再在它下面挑模型**。
 *
 * 一个部署接十几个供应商、每家几十个模型是常态，全部串成一个列表根本找不
 * 到东西。所以做成两栏：左边供应商，右边当前供应商的模型。
 *
 * 已选的模型以标签形式留在选择器外面——跨供应商选完之后，切到别的供应商时
 * 还能看见自己选了什么、能直接删掉。这是两栏方案必须配的一半，不然"看不见
 * 全局"会比长列表更难用。
 */
export function FallbackMultiSelect({ catalog, value, onChange, exclude }: FallbackMultiSelectProps) {
  const selected = React.useMemo(
    () =>
      value
        .split(',')
        .map((v) => v.trim())
        .filter(Boolean),
    [value],
  )

  // 分组保持 catalog 本身的顺序（后端按供应商创建时间排），和模型广场看到的
  // 一致，找起来才有肌肉记忆。
  const groups = React.useMemo<ProviderGroup[]>(() => {
    const byProvider = new Map<string, ProviderGroup>()
    for (const e of catalog) {
      if (`${e.provider}/${e.model}` === exclude) continue
      const bucket = byProvider.get(e.provider) ?? {
        provider: e.provider,
        label: e.provider_display_name,
        items: [],
      }
      bucket.items.push(e)
      byProvider.set(e.provider, bucket)
    }
    return Array.from(byProvider.values())
  }, [catalog, exclude])

  const [activeProvider, setActiveProvider] = React.useState<string | null>(null)
  const active = groups.find((g) => g.provider === activeProvider) ?? groups[0]

  function toggle(ref: string) {
    const next = selected.includes(ref) ? selected.filter((r) => r !== ref) : [...selected, ref]
    onChange(next.join(', '))
  }

  function labelOf(ref: string) {
    return catalog.find((e) => `${e.provider}/${e.model}` === ref)?.display_name ?? ref
  }

  if (groups.length === 0) {
    return <p className="text-body-sm text-ink-500">没有可选的模型——先在 系统配置 → 模型提供商 里添加。</p>
  }

  return (
    <div className="flex flex-col gap-space-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="outline"
            aria-label="选择 Fallback 模型"
            className={cn('h-9 w-full justify-between font-normal', selected.length === 0 && 'text-ink-500')}
          >
            <span className="line-clamp-1">
              {selected.length === 0 ? '选择 Fallback 模型' : `已选 ${selected.length} 个模型`}
            </span>
            <ChevronDownIcon className="size-4 shrink-0 opacity-50" aria-hidden />
          </Button>
        </DropdownMenuTrigger>

        <DropdownMenuContent align="start" className="flex w-[26rem] max-w-[calc(100vw-2rem)] gap-0 p-0">
          {/* 左：供应商。用 DropdownMenuItem 而不是普通按钮，是为了保住菜单
              的键盘导航；preventDefault 让点击只切换分栏、不关闭菜单。 */}
          <ul className="max-h-80 w-40 shrink-0 overflow-y-auto border-r border-border p-1">
            {groups.map((g) => {
              const count = g.items.filter((e) => selected.includes(`${e.provider}/${e.model}`)).length
              return (
                <li key={g.provider}>
                  <DropdownMenuItem
                    onSelect={(e) => e.preventDefault()}
                    onClick={() => setActiveProvider(g.provider)}
                    className={cn('gap-space-2', g.provider === active?.provider && 'bg-blueprint-tint text-blueprint')}
                  >
                    <ProviderIcon name={g.label} className="size-4" />
                    <span className="line-clamp-1 flex-1">{g.label}</span>
                    {count > 0 && <span className="text-caption tabular text-blueprint">{count}</span>}
                  </DropdownMenuItem>
                </li>
              )
            })}
          </ul>

          {/* 右：当前供应商的模型 */}
          <ul className="max-h-80 min-w-0 flex-1 overflow-y-auto p-1">
            {active?.items.map((entry) => {
              const ref = `${entry.provider}/${entry.model}`
              return (
                <li key={ref}>
                  <DropdownMenuCheckboxItem
                    checked={selected.includes(ref)}
                    // 不 preventDefault 的话选一个就关一次，多选变成"开关开关"。
                    onSelect={(e) => e.preventDefault()}
                    onCheckedChange={() => toggle(ref)}
                  >
                    <span className="flex min-w-0 flex-col">
                      <span className="text-body-sm line-clamp-1">{entry.display_name}</span>
                      <span className="text-caption line-clamp-1 text-ink-500">{entry.model}</span>
                    </span>
                  </DropdownMenuCheckboxItem>
                </li>
              )
            })}
          </ul>
        </DropdownMenuContent>
      </DropdownMenu>

      {selected.length > 0 && (
        <ul className="flex flex-wrap gap-space-2">
          {selected.map((ref) => (
            <li key={ref}>
              <span className="text-caption flex items-center gap-1 rounded-full bg-surface-muted py-0.5 pr-1 pl-space-2 text-ink-700">
                {labelOf(ref)}
                <button
                  type="button"
                  aria-label={`移除 ${labelOf(ref)}`}
                  onClick={() => toggle(ref)}
                  className="text-ink-500 hover:text-rust"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
