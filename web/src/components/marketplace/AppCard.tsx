import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Bot, Boxes, Check, Copy, Lock, Plug, Puzzle } from 'lucide-react'
import type { ComponentType } from 'react'

import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']

const TYPE_ICON: Record<string, ComponentType<{ className?: string }>> = {
  bundle: Boxes,
  agent: Bot,
  skill: Puzzle,
  mcp: Plug,
}

const TYPE_TONE: Record<string, string> = {
  bundle: 'bg-blueprint-tint text-blueprint',
  agent: 'bg-signal-tint text-signal',
  skill: 'bg-moss-tint text-moss',
  mcp: 'bg-violet-tint text-violet',
}

/**
 * 广场卡片。解剖来自 design.md 的"发现页"家族：
 *
 *   状态 chip → 标题 → mono ref + 复制 → 描述 → 输入/输出规格条
 *   → 两个 tabular 数字配单位 → 归属行
 *
 * 每一层都只渲染 ListingSummary 里真实存在的字段，没有的就整层不出现
 * （io_description 是可选的，缺了就没有规格条），不摆空占位。
 *
 * 整张卡可点：标题那个 Link 用 ::after 铺满卡片（stretched link），所以
 * 卡片没有嵌套的 <a>——复制按钮是 Link 的兄弟节点、靠 z-10 浮在遮罩之上，
 * 点它不会顺带跳转。把按钮塞进 <a> 里是 HTML 非法且读屏会读错的写法。
 */
export function AppCard({ listing }: { listing: ListingSummary }) {
  const Icon = TYPE_ICON[listing.resource_type] ?? Boxes
  const tone = TYPE_TONE[listing.resource_type] ?? TYPE_TONE.bundle
  const io = listing.display_meta.io_description
  const outputs = io?.outputs ?? []

  return (
    <article className="group relative flex h-full flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-4 transition-colors duration-150 ease-out hover:border-border-strong focus-within:border-blueprint">
      <div className="flex items-start justify-between gap-space-2">
        <span aria-hidden className={`flex size-10 shrink-0 items-center justify-center rounded-sm ${tone}`}>
          <Icon className="size-5" />
        </span>
        <div className="flex shrink-0 items-center gap-space-2">
          {listing.subscribed && (
            <span className="text-caption inline-flex items-center gap-1 rounded-full bg-moss-tint px-space-2 py-0.5 text-moss">
              <span aria-hidden className="size-1.5 rounded-full bg-moss" />
              已订阅
            </span>
          )}
          <span
            className="text-caption inline-flex items-center gap-1 rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700"
            title="黑盒分发：订阅后可以运行，但看不到内部定义"
          >
            <Lock className="size-3" aria-hidden />
            黑盒
          </span>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-1">
        <h3 className="text-label-md text-ink-900">
          <Link
            to={`/marketplace/listing/${listing.listing_ref}`}
            className="line-clamp-1 after:absolute after:inset-0 after:content-[''] hover:text-blueprint"
          >
            {listing.display_meta.display_name}
          </Link>
        </h3>
        <RefWithCopy value={listing.listing_ref} />
      </div>

      <p className="text-body-sm line-clamp-2 min-h-[2.6em] text-ink-700">{listing.display_meta.description}</p>

      {(io?.input || outputs.length > 0) && (
        <dl className="text-caption flex flex-col gap-1 border-t border-border pt-space-3 text-ink-500">
          {io?.input && (
            <div className="flex gap-space-2">
              <dt className="shrink-0">输入</dt>
              <dd className="line-clamp-1 min-w-0 text-ink-700">{io.input}</dd>
            </div>
          )}
          {outputs.length > 0 && (
            <div className="flex gap-space-2">
              <dt className="shrink-0">输出</dt>
              <dd className="line-clamp-1 min-w-0 text-ink-700">
                {outputs.map((o) => (
                  <span key={o} className="text-ref mr-space-2 text-ink-700">
                    {o}
                  </span>
                ))}
              </dd>
            </div>
          )}
        </dl>
      )}

      {/* mt-auto：规格条是可选的，没有它的卡片会短一截，数字行和归属行就
          和邻居错开——一排卡片里横向的带对不齐，看着就是没收拾干净。把这
          两行推到卡片底部，不管上面有没有规格条都在同一条基线上。
          数字刻意不做成参考站那么大：那边的"上下文/最大输出"是选型时真要
          比的参数，这里的订阅/运行数没有那个决策分量。 */}
      <div className="mt-auto grid grid-cols-2 gap-space-3 border-t border-border pt-space-3">
        <Figure value={listing.subscriber_count} label="订阅" />
        <Figure value={listing.run_count ?? 0} label="运行" />
      </div>

      <div className="text-caption flex items-center justify-between gap-space-2 text-ink-500">
        <span className="truncate">@{listing.author.display_name}</span>
        <span className="text-ref shrink-0">v{listing.version}</span>
      </div>
    </article>
  )
}

function Figure({ value, label }: { value: number; label: string }) {
  return (
    <div className="flex flex-col">
      <span className="text-display-sm tabular text-ink-900">{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

/**
 * ref 是给人复制去填 bundle_ref 的，之前只能手抄。复制这个动作的结果本身
 * 不可见，所以给一次就地反馈（图标换成对勾 1.2s）——不是弹 toast：toast
 * 留给失败和效果不可见的异步动作，一次剪贴板写入不值得占用它。
 */
function RefWithCopy({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => () => clearTimeout(timer.current), [])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 1200)
    } catch {
      // 剪贴板不可用（非安全上下文、权限被拒）时不谎报成功——ref 本身就在
      // 屏幕上，用户还能手选。
    }
  }

  return (
    <span className="flex min-w-0 items-center gap-1">
      <code className="text-ref truncate text-ink-500">{value}</code>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? `已复制 ${value}` : `复制 ${value}`}
        className="relative z-10 shrink-0 rounded-xs p-1 text-ink-500 transition-colors duration-150 ease-out hover:bg-surface-muted hover:text-ink-900"
      >
        {copied ? <Check className="size-3.5 text-moss" aria-hidden /> : <Copy className="size-3.5" aria-hidden />}
      </button>
    </span>
  )
}
