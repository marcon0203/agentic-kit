import * as React from 'react'

import { cn } from '@/lib/utils'

interface MarketAvatarProps {
  /** 上游给的图标地址；绝大多数条目没有，这是常态。 */
  iconUrl?: string
  /** 生成兜底图标的依据，用条目的 slug/限定名——同一个条目每次都长一样。 */
  seed: string
  /** 取首字母用的显示名；缺省用 seed。 */
  name?: string
  className?: string
}

/* 兜底配色。挑的是本设计系统里已有的那几个色相的浅底+深字组合，避免市场页
   出现一堆设计系统之外的随机颜色——彩色的目的是让人能一眼区分条目，不是
   让页面变花。 */
const PALETTE = [
  'bg-blueprint-tint text-blueprint',
  'bg-moss-tint text-moss',
  'bg-signal-tint text-signal',
  'bg-rust-tint text-rust',
  'bg-surface-muted text-ink-700',
]

/** 稳定的字符串散列：同一个 slug 每次都落到同一个颜色，翻页不会变来变去。 */
function hashOf(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

/**
 * 取一个能代表条目的字。限定名形如 io.github.owner/airtable-mcp-server，
 * 有意义的是最后一段而不是 "io"——按域名前缀取首字母的话，半个市场的图标
 * 都会是同一个 "I"。
 */
function initialOf(seed: string, name?: string): string {
  const source = (name && !name.includes('/') ? name : seed.split('/').pop()) ?? seed
  const cleaned = source.replace(/^[^\p{L}\p{N}]+/u, '')
  return (cleaned[0] ?? '?').toUpperCase()
}

/**
 * 市场条目的头像：有图用图，没图按 slug 生成一个字母图标。
 *
 * 兜底不是锦上添花而是主路径——上游给图标的条目是少数，只接 icon_url 的话
 * 绝大多数卡片仍然空着一块。图片加载失败（防盗链、图挂了、混合内容被拦）
 * 同样退回字母图标，不留一个碎图标。
 */
export function MarketAvatar({ iconUrl, seed, name, className }: MarketAvatarProps) {
  const [failed, setFailed] = React.useState(false)

  // 换了条目要把失败状态清掉，否则列表复用节点时上一条的失败会粘在新条目上。
  React.useEffect(() => setFailed(false), [iconUrl])

  const base = 'flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-md'

  if (iconUrl && !failed) {
    return (
      <img
        src={iconUrl}
        alt=""
        aria-hidden
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className={cn(base, 'bg-surface-muted object-cover', className)}
      />
    )
  }

  return (
    <span aria-hidden className={cn(base, 'text-label-md font-medium', PALETTE[hashOf(seed) % PALETTE.length], className)}>
      {initialOf(seed, name)}
    </span>
  )
}
