import * as React from 'react'
import { ChevronDownIcon } from 'lucide-react'

import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
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

/**
 * Fallback 模型多选。
 *
 * 之前是拿 Radix Select 拼的，点不动——Select.Item 没有 onSelect 这个 prop
 * （那是 DropdownMenu 的），传进去只会被当成未知 DOM 属性丢掉，所以点击什
 * 么都不会发生；而 Root 的 onValueChange 又被写成了空函数。这里换成
 * DropdownMenu.CheckboxItem，它本来就是"可多选、选完不关"的原语。
 *
 * 列表按供应商分组：降级链的每一项都是 `provider/模型名`，不按供应商分组
 * 的话，几十个模型平铺出来根本认不出哪个是哪家的。
 */
export function FallbackMultiSelect({ catalog, value, onChange, exclude }: FallbackMultiSelectProps) {
  const selected = value
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean)

  const options = React.useMemo(
    () => catalog.filter((e) => `${e.provider}/${e.model}` !== exclude),
    [catalog, exclude],
  )

  // 分组保持 catalog 本身的顺序（后端按供应商创建时间排），不重新排序——
  // 广场上看到的顺序和这里一致，找起来才有肌肉记忆。
  const groups = React.useMemo(() => {
    const byProvider = new Map<string, { label: string; items: ModelCatalogEntry[] }>()
    for (const e of options) {
      const bucket = byProvider.get(e.provider) ?? { label: e.provider_display_name, items: [] }
      bucket.items.push(e)
      byProvider.set(e.provider, bucket)
    }
    return Array.from(byProvider.entries())
  }, [options])

  function toggle(ref: string) {
    const next = selected.includes(ref) ? selected.filter((r) => r !== ref) : [...selected, ref]
    onChange(next.join(', '))
  }

  const displayText =
    selected.length === 0
      ? '选择 Fallback 模型'
      : selected.length === 1
        ? (catalog.find((e) => `${e.provider}/${e.model}` === selected[0])?.display_name ?? selected[0])
        : `已选 ${selected.length} 个模型`

  if (options.length === 0) {
    return <p className="text-body-sm text-ink-500">没有可选的模型——先在 系统配置 → 模型提供商 里添加。</p>
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          aria-label="选择 Fallback 模型"
          className={cn('h-9 w-full justify-between font-normal', selected.length === 0 && 'text-ink-500')}
        >
          <span className="line-clamp-1">{displayText}</span>
          <ChevronDownIcon className="size-4 shrink-0 opacity-50" aria-hidden />
        </Button>
      </DropdownMenuTrigger>

      <DropdownMenuContent
        align="start"
        className="max-h-[min(24rem,var(--radix-dropdown-menu-content-available-height))] w-[var(--radix-dropdown-menu-trigger-width)] overflow-y-auto"
      >
        {groups.map(([provider, group], i) => (
          <React.Fragment key={provider}>
            {i > 0 && <DropdownMenuSeparator />}
            <DropdownMenuLabel className="flex items-center gap-space-2">
              <ProviderIcon name={group.label} className="size-4" />
              {group.label}
            </DropdownMenuLabel>
            {group.items.map((entry) => {
              const ref = `${entry.provider}/${entry.model}`
              return (
                <DropdownMenuCheckboxItem
                  key={ref}
                  checked={selected.includes(ref)}
                  // 不 preventDefault 的话选一个就关一次，多选变成"开关开关"。
                  onSelect={(e) => e.preventDefault()}
                  onCheckedChange={() => toggle(ref)}
                >
                  <span className="flex flex-col">
                    <span className="text-body-sm">{entry.display_name}</span>
                    <span className="text-caption text-ink-500">{ref}</span>
                  </span>
                </DropdownMenuCheckboxItem>
              )
            })}
          </React.Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
