import { Link } from 'react-router-dom'
import { Bot, Boxes, Lock, Plug, Puzzle, Users, PlayCircle } from 'lucide-react'
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
 * A广场卡片：图标方块 + 标题 + 使用量一行 + 简介，呼应截图里应用广场的卡片
 * 样式。数据上只如实展示这个平台真正有的两个量——订阅数与运行次数——
 * 而不是照搬截图里的"浏览/复制"（那两个量在黑盒分发模型里没有对应物）。
 */
export function AppCard({ listing }: { listing: ListingSummary }) {
  const Icon = TYPE_ICON[listing.resource_type] ?? Boxes
  const tone = TYPE_TONE[listing.resource_type] ?? TYPE_TONE.bundle

  return (
    <Link
      to={`/marketplace/listing/${listing.listing_ref}`}
      className="flex items-start gap-space-4 rounded-lg border border-border bg-surface p-space-4 transition-colors hover:border-border-strong"
    >
      <span
        aria-hidden
        className={`flex size-11 shrink-0 items-center justify-center rounded-md ${tone}`}
      >
        <Icon className="size-5" />
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-center gap-space-2">
          <span className="text-body-md truncate font-medium text-ink-900">
            {listing.display_meta.display_name}
          </span>
          {listing.subscribed && (
            <span className="text-caption inline-flex shrink-0 items-center gap-1 text-moss">
              <span aria-hidden className="size-1.5 rounded-full bg-moss" />
              已订阅
            </span>
          )}
        </span>
        <span className="text-caption flex items-center gap-space-3 text-ink-500">
          <span className="inline-flex items-center gap-1">
            <Users className="size-3" aria-hidden />
            {listing.subscriber_count}
          </span>
          <span className="inline-flex items-center gap-1">
            <PlayCircle className="size-3" aria-hidden />
            {listing.run_count ?? 0}
          </span>
          <span className="inline-flex items-center gap-1" title="黑盒分发：订阅后可以运行，但看不到内部定义">
            <Lock className="size-3" aria-hidden />
            黑盒
          </span>
        </span>
        <span className="text-caption line-clamp-2 text-ink-700">
          {listing.display_meta.description}
        </span>
      </span>
    </Link>
  )
}
