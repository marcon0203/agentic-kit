import * as React from 'react'

import { cn } from '@/lib/utils'

/**
 * 供应商图标。
 *
 * 图标来自 @lobehub/icons-static-svg——同一套厂商图标的纯 SVG 资源包，零依
 * 赖。不用 @lobehub/icons 是因为后者是 antd + React 组件包，会把 antd 和
 * @lobehub/ui 拖进这个 Tailwind + shadcn 的项目。
 *
 * glob 是 **eager** 的，拿到的是一张「文件名 → 资源 URL」的表。配合
 * vite.config.ts 里对这些图标关掉内联，表里存的是 "/assets/kimi-color-xxx.svg"
 * 这样的短字符串，而不是 data URI——整张表约 130KB 纯文本，gzip 后很小，
 * 图标本身是 900 个静态文件，用到哪个浏览器取哪个。
 *
 * 这里**不能**用 lazy glob：那样每个图标会变成一个独立的 JS chunk，运行时
 * 动态 import，取不到就静默什么都不显示。之前"选了图标没加载出来"就是这么
 * 来的。
 */
const ICON_URLS = import.meta.glob<string>('/node_modules/@lobehub/icons-static-svg/icons/*.svg', {
  query: '?url',
  import: 'default',
  eager: true,
})

/** ".../icons/kimi-color.svg" → "kimi-color" */
function baseNameOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1).replace(/\.svg$/, '')
}

/** 文件名 → URL，查表用，省得每次去扫 900 条路径。 */
const BY_BASENAME: Record<string, string> = Object.fromEntries(
  Object.entries(ICON_URLS).map(([p, url]) => [baseNameOf(p), url]),
)

/**
 * 可填的图标名清单（去掉 -color/-text/-brand 后缀、去重、排序），给输入框
 * 做补全。
 */
export const LOBEHUB_ICON_NAMES: string[] = Array.from(
  new Set(Object.keys(BY_BASENAME).map((n) => n.replace(/-(color|text|brand)$/, ''))),
).sort()

/** 同名优先取彩色版，没有再退回单色版。 */
export function lobehubIconURL(name: string | null | undefined): string | undefined {
  const wanted = (name ?? '').trim().toLowerCase()
  if (!wanted) return undefined
  for (const suffix of ['-color', '', '-brand', '-text']) {
    const hit = BY_BASENAME[wanted + suffix]
    if (hit) return hit
  }
  return undefined
}

export function isLobehubIconName(value: string): boolean {
  return lobehubIconURL(value) !== undefined
}

/**
 * 把 icon 字段解析成能直接放进 <img src> 的地址。
 *
 * icon 有三种可能：一个 http(s)/data: 地址（管理员自己的图）、一个 lobehub
 * 图标名（"kimi"、"zhipu"），或者空——空时按协议模板名再试一次，deepseek
 * 模板正好配上 deepseek 图标。
 */
export function resolveIconSrc(
  icon: string | null | undefined,
  template: string | null | undefined,
): string | undefined {
  const raw = (icon ?? '').trim()
  if (/^(https?:|data:|\/)/.test(raw)) return raw
  if (raw) return lobehubIconURL(raw)
  return lobehubIconURL(template)
}

export function ProviderIcon({
  template,
  icon,
  name,
  className,
}: {
  /** 协议模板 id。icon 为空时拿它当图标名试一次。 */
  template?: string | null
  /** 管理员填的：http(s)/data: 地址，或一个 lobehub 图标名 */
  icon?: string | null
  /** 都取不到时用首字母兜底 */
  name: string
  className?: string
}) {
  const src = resolveIconSrc(icon, template)
  // 图挂了（外链失效、资源没部署上）也要退回首字母，不留一个碎图标。
  const [failed, setFailed] = React.useState(false)
  React.useEffect(() => setFailed(false), [src])

  if (src && !failed) {
    return (
      <img
        src={src}
        alt=""
        onError={() => setFailed(true)}
        className={cn('size-8 shrink-0 rounded-sm object-contain', className)}
      />
    )
  }
  return (
    <span
      aria-hidden
      className={cn(
        'text-caption flex size-8 shrink-0 items-center justify-center rounded-sm bg-surface-muted font-medium text-ink-500',
        className,
      )}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  )
}
