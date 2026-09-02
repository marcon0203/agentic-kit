import type { ComponentType } from 'react'
import { cn } from '@/lib/utils'

export interface SectionSidebarItem {
  value: string
  label: string
  icon: ComponentType<{ className?: string }>
}

export interface SectionSidebarGroup {
  label?: string
  items: SectionSidebarItem[]
}

/**
 * 二级导航：顶部横向导航栏是一级菜单（在哪个中心），这一列是二级菜单（中心
 * 内部的哪个模块）。做成通用组件是因为这个两级结构不会只用在应用广场一处。
 *
 * 接受 `items`（单组，不带分组标题）或 `groups`（多组，每组一个可选的灰色
 * 小标题）——两者选一个传。
 */
export function SectionSidebar({
  items,
  groups,
  active,
  onChange,
}: {
  items?: SectionSidebarItem[]
  groups?: SectionSidebarGroup[]
  active: string
  onChange: (value: string) => void
}) {
  const resolvedGroups = groups ?? [{ items: items ?? [] }]

  return (
    <nav
      aria-label="模块导航"
      className={cn(
        'flex shrink-0 flex-row gap-space-5 overflow-x-auto sm:w-48 sm:flex-col',
        // 二级菜单跟着长列表一起滚走的话，翻到页尾就没法换模块了。粘在顶
        // 栏（h-14）下面：self-start 让它按内容高度收缩，否则作为被拉伸的
        // flex item 它已经占满容器，sticky 没有可移动的空间；自身太高时
        // 内部滚动，不去挤主内容区。
        'sm:sticky sm:top-20 sm:max-h-[calc(100vh-7rem)] sm:self-start sm:overflow-y-auto',
      )}
    >
      {resolvedGroups.map((group, i) => (
        <div key={group.label ?? i} className="flex shrink-0 flex-row gap-space-1 sm:flex-col">
          {group.label && (
            <span className="text-caption hidden px-space-3 text-ink-500 sm:block">{group.label}</span>
          )}
          {group.items.map((item) => {
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
        </div>
      ))}
    </nav>
  )
}
