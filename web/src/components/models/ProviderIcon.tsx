import * as React from 'react'

import { cn } from '@/lib/utils'

/**
 * 供应商图标。
 *
 * 图标来自 @lobehub/icons-static-svg——同一套厂商图标的纯 SVG 资源包，零依
 * 赖。不用 @lobehub/icons 是因为后者是 antd + React 组件包，会把 antd 和
 * @lobehub/ui 拖进这个 Tailwind + shadcn 的项目。
 *
 * 900 多个图标**按需加载**：eager 的话 Vite 会把小 SVG 内联进 JS，几百 KB
 * 全压在首屏上，而一次只用得到一两个。这里 glob 出来的是一组 import 函数，
 * 真正引用到哪个才去取哪个。
 */
const ICON_LOADERS = import.meta.glob<string>('/node_modules/@lobehub/icons-static-svg/icons/*.svg', {
  query: '?url',
  import: 'default',
})

/** 从 glob 的路径键里取出图标名：".../icons/kimi-color.svg" → "kimi-color"。 */
function baseNameOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1).replace(/\.svg$/, '')
}

/**
 * 可填的图标名清单（去掉 -color/-text 后缀、去重、排序），给输入框做补全。
 *
 * 只用到 glob 的**键**，不加载任何资源，所以这份清单本身是免费的。
 */
export const LOBEHUB_ICON_NAMES: string[] = Array.from(
  new Set(Object.keys(ICON_LOADERS).map((p) => baseNameOf(p).replace(/-(color|text|brand)$/, ''))),
).sort()

/** 同一个名字优先取彩色版，没有再退回单色版。 */
function loaderFor(name: string): (() => Promise<string>) | undefined {
  const wanted = name.trim().toLowerCase()
  if (!wanted) return undefined
  for (const suffix of ['-color', '', '-brand', '-text']) {
    const hit = Object.keys(ICON_LOADERS).find((p) => baseNameOf(p) === wanted + suffix)
    if (hit) return ICON_LOADERS[hit]
  }
  return undefined
}

/** 已解析过的图标，避免同一个名字在列表里反复触发动态 import。 */
const resolved = new Map<string, string>()

export function isLobehubIconName(value: string): boolean {
  return loaderFor(value) !== undefined
}

/**
 * 把 icon 字段解析成一个能直接放进 <img src> 的地址。
 *
 * icon 可以是三种东西：一个 http(s)/data: 地址（管理员自己的图）、一个
 * lobehub 图标名（"kimi"、"zhipu"），或者空——空时退回按协议模板猜。
 */
function useIconSrc(icon: string | null | undefined, template: string | null | undefined): string | undefined {
  const raw = (icon ?? '').trim()
  const isURL = /^(https?:|data:|\/)/.test(raw)
  // 没填就按模板名当图标名试一次：deepseek 模板配 deepseek 图标，正好对得上。
  const name = raw && !isURL ? raw : raw ? '' : (template ?? '')

  const [src, setSrc] = React.useState<string | undefined>(() =>
    isURL ? raw : resolved.get(name.trim().toLowerCase()),
  )

  React.useEffect(() => {
    if (isURL) {
      setSrc(raw)
      return
    }
    const key = name.trim().toLowerCase()
    const cached = resolved.get(key)
    if (cached) {
      setSrc(cached)
      return
    }
    const load = loaderFor(key)
    if (!load) {
      setSrc(undefined)
      return
    }
    let alive = true
    void load().then((url) => {
      resolved.set(key, url)
      if (alive) setSrc(url)
    })
    return () => {
      alive = false
    }
  }, [raw, isURL, name])

  return src
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
  const src = useIconSrc(icon, template)
  if (src) {
    return <img src={src} alt="" className={cn('size-8 shrink-0 rounded-sm object-contain', className)} />
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
