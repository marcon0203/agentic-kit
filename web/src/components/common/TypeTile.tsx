import type { ComponentType } from 'react'
import { cn } from '@/lib/utils'

/**
 * 类型色相环（design-system.md v3.0 §1.4）——每种资源一个固定色相，
 * tint 做底、solid 做图标/文字。它是卡片和列表行的视觉锚点：没有它，
 * 一排卡片读起来就是一堆同权重的灰字。色相刻意避开状态色
 * （moss 成功 / signal 待审 / rust 失败），类型色不承载状态含义。
 */
export const TYPE_TONES = {
  bundle: 'bg-type-bundle-tint text-type-bundle',
  agent: 'bg-type-agent-tint text-type-agent',
  skill: 'bg-type-skill-tint text-type-skill',
  tool: 'bg-type-tool-tint text-type-tool',
  mcp: 'bg-type-mcp-tint text-type-mcp',
  lib: 'bg-type-lib-tint text-type-lib',
} as const

export type TypeTone = keyof typeof TYPE_TONES

/**
 * 图标方块：卡片、列表行、空态共用的彩色锚点。
 * 尺寸三档——行内 sm(32)、卡片 md(44)、空态/hero lg(56)。
 */
export function TypeTile({
  icon: Icon,
  tone,
  size = 'md',
  className,
}: {
  icon: ComponentType<{ className?: string }>
  tone: TypeTone
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  const box = {
    sm: 'size-8 rounded-sm [&_svg]:size-4',
    md: 'size-11 rounded-md [&_svg]:size-5',
    lg: 'size-14 rounded-lg [&_svg]:size-6',
  }[size]

  return (
    <span aria-hidden className={cn('flex shrink-0 items-center justify-center', box, TYPE_TONES[tone], className)}>
      <Icon />
    </span>
  )
}
