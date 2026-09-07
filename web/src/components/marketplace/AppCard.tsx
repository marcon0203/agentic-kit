import { Link } from 'react-router-dom'
import { Bot, Boxes, Lock, Plug, Puzzle, Users, PlayCircle } from 'lucide-react'
import type { ComponentType } from 'react'

import type { components } from '@/lib/api/schema'
import { TYPE_TONES } from '@/components/common/TypeTile'

type ListingSummary = components['schemas']['ListingSummary']

const TYPE_ICON: Record<string, ComponentType<{ className?: string }>> = {
  bundle: Boxes,
  agent: Bot,
  skill: Puzzle,
  mcp: Plug,
}

/* The type hue wheel (design-system.md v3.0 §1.4): each resource type keeps
   one hue everywhere it appears — tile, top spine, group header. Hues sit
   away from the status colours, so a green tile can never read as "done". */
const TYPE_TONE: Record<string, string> = {
  bundle: TYPE_TONES.bundle,
  agent: TYPE_TONES.agent,
  skill: TYPE_TONES.skill,
  mcp: TYPE_TONES.mcp,
}

const TYPE_SPINE: Record<string, string> = {
  bundle: 'border-t-type-bundle',
  agent: 'border-t-type-agent',
  skill: 'border-t-type-skill',
  mcp: 'border-t-type-mcp',
}

/**
 * 广场卡片：类型色顶脊 + 图标方块 + 标题 + 使用量一行 + 简介。顶脊让一屏
 * 卡片的类型构成一眼可读——不用读字就知道这排是 Skill、那排是 Bundle。
 * 数据上只如实展示这个平台真正有的两个量——订阅数与运行次数——而不是
 * 照搬截图里的"浏览/复制"（那两个量在黑盒分发模型里没有对应物）。
 */
export function AppCard({ listing }: { listing: ListingSummary }) {
  const Icon = TYPE_ICON[listing.resource_type] ?? Boxes
  const tone = TYPE_TONE[listing.resource_type] ?? TYPE_TONE.bundle
  const spine = TYPE_SPINE[listing.resource_type] ?? TYPE_SPINE.bundle

  return (
    <Link
      to={`/marketplace/listing/${listing.listing_ref}`}
      className={`flex items-start gap-space-4 rounded-lg border border-border border-t-2 bg-surface p-space-4 transition-all hover:border-blueprint-edge hover:shadow-status-sm ${spine}`}
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
            <span className="text-caption inline-flex shrink-0 items-center gap-1 rounded-full bg-moss-tint px-space-2 py-0.5 text-moss-deep">
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
