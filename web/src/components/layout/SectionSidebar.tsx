import type { ComponentType } from 'react'
import { cn } from '@/lib/utils'

export interface SectionSidebarItem {
  value: string
  label: string
  icon: ComponentType<{ className?: string }>
}

/**
 * 二级导航：顶部横向导航栏是一级菜单（在哪个中心），这一列是二级菜单（中心
 * 内部的哪个模块）。做成通用组件是因为这个两级结构不会只用在应用广场一处。
 */
export function SectionSidebar({
  items,
  active,
  onChange,
}: {
  items: SectionSidebarItem[]
  active: string
  onChange: (value: string) => void
}) {
  return (
    <nav
      aria-label="模块导航"
      className="flex shrink-0 flex-row gap-space-1 overflow-x-auto border-b border-border pb-space-3 sm:w-44 sm:flex-col sm:border-b-0 sm:border-r sm:pb-0 sm:pr-space-4"
    >
      {items.map((item) => {
        const Icon = item.icon
        const isActive = item.value === active
        return (
          <button
            key={item.value}
            type="button"
            onClick={() => onChange(item.value)}
            className={cn(
              'text-body-sm flex shrink-0 items-center gap-space-2 rounded-sm px-space-3 py-space-2 text-left transition-colors',
              isActive
                ? 'bg-blueprint-tint font-medium text-blueprint'
                : 'text-ink-700 hover:bg-surface-muted hover:text-ink-900',
            )}
          >
            <Icon className="size-4 shrink-0" aria-hidden />
            {item.label}
          </button>
        )
      })}
    </nav>
  )
}
