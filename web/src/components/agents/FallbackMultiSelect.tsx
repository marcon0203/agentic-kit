import * as React from 'react'
import * as SelectPrimitive from '@radix-ui/react-select'
import { ChevronDownIcon } from 'lucide-react'

import { cn } from '@/lib/utils'
import { Checkbox } from '@/components/ui/checkbox'
import type { components } from '@/lib/api/schema'

type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']

interface FallbackMultiSelectProps {
  catalog: ModelCatalogEntry[]
  value: string
  onChange: (v: string) => void
}

export function FallbackMultiSelect({ catalog, value, onChange }: FallbackMultiSelectProps) {
  const [open, setOpen] = React.useState(false)
  const selected = value
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean)

  function toggle(ref: string) {
    const next = selected.includes(ref) ? selected.filter((r) => r !== ref) : [...selected, ref]
    onChange(next.join(', '))
  }

  const displayText =
    selected.length === 0
      ? '选择 Fallback 模型'
      : selected.length === 1
        ? catalog.find((e) => `${e.provider}/${e.model}` === selected[0])?.display_name ?? selected[0]
        : `已选 ${selected.length} 个模型`

  if (catalog.length === 0) {
    return <p className="text-body-sm text-ink-500">模型广场里还没有登记任何模型。</p>
  }

  return (
    <SelectPrimitive.Root value="" onValueChange={() => {}} open={open} onOpenChange={setOpen}>
      <SelectPrimitive.Trigger
        aria-label="选择 Fallback 模型"
        className={cn(
          "border-input data-[placeholder]:text-muted-foreground [&_svg:not([class*='text-'])]:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 aria-invalid:border-destructive flex w-full items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 h-9",
        )}
      >
        <span className="line-clamp-1">{displayText}</span>
        <SelectPrimitive.Icon asChild>
          <ChevronDownIcon className="size-4 shrink-0 opacity-50" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>

      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          className={cn(
            'bg-popover text-popover-foreground data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 relative z-50 max-h-[min(24rem,var(--radix-select-content-available-height))] min-w-[8rem] origin-[--radix-select-content-transform-origin] overflow-hidden rounded-md border shadow-md',
            'data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1',
          )}
        >
          <SelectPrimitive.Viewport className="h-[var(--radix-select-trigger-height)] w-full min-w-[var(--radix-select-trigger-width)] p-1">
            {catalog.map((entry) => {
              const ref = `${entry.provider}/${entry.model}`
              const checked = selected.includes(ref)
              return (
                <SelectPrimitive.Item
                  key={ref}
                  value={ref}
                  onSelect={(e) => {
                    e.preventDefault()
                    toggle(ref)
                  }}
                  className={cn(
                    'relative flex w-full cursor-default items-center gap-3 rounded-sm py-2 pr-8 pl-2 text-sm outline-hidden select-none',
                    'focus:bg-accent focus:text-accent-foreground',
                  )}
                >
                  <Checkbox checked={checked} tabIndex={-1} aria-hidden="true" />
                  <span className="flex flex-col">
                    <span className="text-body-sm">{entry.display_name}</span>
                    <span className="text-caption text-ink-500">{ref}</span>
                  </span>
                </SelectPrimitive.Item>
              )
            })}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}
