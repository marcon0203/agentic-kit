import { Pencil, Play, Trash2, Workflow, GitBranch, Box, Rocket } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import { TypeTile } from '@/components/common/TypeTile'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type Bundle = components['schemas']['Bundle']
type BundleDefinition = components['schemas']['BundleDefinition']

/** What each run type is, in one glance — the same wording the editor's
 * properties panel uses, shortened to a badge. */
const RUN_TYPE_META = {
  graph: { label: '图编排', Icon: GitBranch },
  flow: { label: '顺序流程', Icon: Workflow },
  single: { label: '单体', Icon: Box },
} as const

interface BundleCardProps {
  bundle: Bundle
  runBlocked: boolean
  onRun: (ref: string) => void
  onEdit: (ref: string) => void
  onDelete: (ref: string) => void
  onPublish: (bundle: Bundle) => void
}

/**
 * 应用中心的卡片。图标方块（类型色相环的 bundle 紫）是整张卡的视觉锚点，
 * 状态用 tint chip 而不是裸圆点——颜色从来不单独承载状态，这里补上了文字。
 */
export function BundleCard({ bundle, runBlocked, onRun, onEdit, onDelete, onPublish }: BundleCardProps) {
  const definition = bundle.definition as BundleDefinition
  const runType = RUN_TYPE_META[definition.type ?? 'graph'] ?? RUN_TYPE_META.graph
  const agentCount = definition.agents?.length ?? 0
  const enabled = bundle.status === 1

  return (
    <div className="group flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-5 transition-all hover:border-blueprint-edge hover:shadow-status-sm">
      <div className="flex items-start gap-space-3">
        <TypeTile icon={runType.Icon} tone="bundle" />
        <div className="flex min-w-0 flex-1 flex-col gap-space-2">
          <span className="flex items-center gap-space-2">
            <Ref>{bundle.bundle_ref}</Ref>
            <span className="text-caption tabular text-ink-500">v{bundle.version}</span>
            <span
              className={cn(
                'text-caption ml-auto inline-flex w-max items-center gap-1 rounded-full px-space-2 py-0.5',
                enabled ? 'bg-moss-tint text-moss-deep' : 'bg-surface-muted text-ink-500',
              )}
            >
              <span aria-hidden className={cn('size-1.5 rounded-full', enabled ? 'bg-moss' : 'bg-border-strong')} />
              {enabled ? '已启用' : '已停用'}
            </span>
          </span>
          <span className="text-caption flex items-center gap-space-3 text-ink-500">
            <span className="inline-flex items-center gap-1">
              <runType.Icon className="size-3" aria-hidden />
              {runType.label}
            </span>
            <span className="tabular">{agentCount} 个智能体</span>
          </span>
        </div>
      </div>

      <p className="text-body-sm line-clamp-2 min-h-[2.5rem] text-ink-500">
        {definition.description || '还没有写描述。'}
      </p>

      <div className="mt-auto flex items-center gap-space-2">
        <Button
          size="sm"
          disabled={runBlocked}
          title={runBlocked ? '先去模型广场接入一个 Provider，才能发起运行' : undefined}
          onClick={() => onRun(bundle.bundle_ref)}
        >
          <Play className="mr-1 size-3.5" aria-hidden />
          运行
        </Button>
        <Button variant="outline" size="sm" onClick={() => onEdit(bundle.bundle_ref)}>
          <Pencil className="mr-1 size-3.5" aria-hidden />
          编辑
        </Button>
        <Button variant="outline" size="sm" onClick={() => onPublish(bundle)}>
          <Rocket className="mr-1 size-3.5" aria-hidden />
          发布
        </Button>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`删除 ${bundle.bundle_ref}`}
          className="ml-auto text-ink-500 hover:text-rust"
          onClick={() => onDelete(bundle.bundle_ref)}
        >
          <Trash2 className="size-3.5" aria-hidden />
        </Button>
      </div>
    </div>
  )
}
