import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ExternalLink, X } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Section } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MarketSkill = components['schemas']['MarketSkill']
type ReviewStatus = components['schemas']['SkillReviewStatus']

const STATUS_META: Record<ReviewStatus, { label: string; className: string }> = {
  pending: { label: '待审核', className: 'bg-signal-tint text-signal' },
  approved: { label: '已通过', className: 'bg-moss-tint text-moss' },
  rejected: { label: '已驳回', className: 'bg-rust-tint text-rust' },
}

type Filter = ReviewStatus | 'all'

/**
 * 系统配置 → Skill 源 → 审核台：同步进来的全部条目在这里过一遍，只有通过
 * 的才会出现在 Skill 管理的市场视图、也才允许安装。
 *
 * 批量是主路径而不是附加功能：一个公开源同步下来动辄成百上千条，逐条点
 * 审不完——所以有全选、有按状态筛，通过/驳回都作用在整个选择上。
 */
export function SkillReviewPanel() {
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<Filter>('pending')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const query = useQuery({
    queryKey: ['skill-review', filter],
    queryFn: async () =>
      unwrap<{ items: MarketSkill[]; counts: Record<ReviewStatus, number> }>(
        await apiClient.GET('/skill-sources/skills', {
          params: { query: filter === 'all' ? {} : { review_status: filter } },
        }),
      ),
  })

  const items = useMemo(() => {
    const all = query.data?.items ?? []
    const q = search.trim().toLowerCase()
    if (!q) return all
    return all.filter(
      (s) => s.slug.toLowerCase().includes(q) || s.name.toLowerCase().includes(q) || (s.summary ?? '').toLowerCase().includes(q),
    )
  }, [query.data, search])

  const counts = query.data?.counts
  const keyOf = (s: MarketSkill) => `${s.source_id}/${s.slug}`
  const allSelected = items.length > 0 && items.every((s) => selected.has(keyOf(s)))

  const reviewMutation = useMutation({
    mutationFn: async ({ status, targets }: { status: ReviewStatus; targets: MarketSkill[] }) =>
      unwrap<{ reviewed: number }>(
        await apiClient.POST('/skill-sources/skills/review', {
          body: {
            status,
            items: targets.map((s) => ({ source_id: Number(s.source_id), slug: s.slug })),
          },
        }),
      ),
    onSuccess: (res, vars) => {
      toast.success(`${vars.status === 'approved' ? '已通过' : '已驳回'} ${res.reviewed} 个 Skill`)
      setSelected(new Set())
      queryClient.invalidateQueries({ queryKey: ['skill-review'] })
      // 市场视图只显示通过的条目，审核结果要立刻反映过去。
      queryClient.invalidateQueries({ queryKey: ['skill-market'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '审核没能完成，请再试一次'),
  })

  function toggle(skill: MarketSkill) {
    const key = keyOf(skill)
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function toggleAll() {
    setSelected(allSelected ? new Set() : new Set(items.map(keyOf)))
  }

  const selectedItems = items.filter((s) => selected.has(keyOf(s)))

  return (
    <Section
      title="Skill 审核"
      aside={
        <div className="flex items-center gap-space-2">
          {(['pending', 'approved', 'rejected', 'all'] as Filter[]).map((f) => (
            <Button
              key={f}
              variant={filter === f ? 'secondary' : 'ghost'}
              size="sm"
              onClick={() => {
                setFilter(f)
                setSelected(new Set())
              }}
            >
              {f === 'all' ? '全部' : STATUS_META[f].label}
              {counts && f !== 'all' && <span className="text-caption tabular ml-1 text-ink-500">{counts[f] ?? 0}</span>}
            </Button>
          ))}
        </div>
      }
    >
      <p className="text-body-sm text-ink-500">
        同步进来的 Skill 默认待审核，只有通过审核的才会出现在「Skill 管理 → 市场」里供用户安装。
      </p>

      {query.isLoading && <ListSkeleton />}
      {query.isError && <ErrorPanel message="Skill 列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyRail
          title={filter === 'pending' ? '没有待审核的 Skill' : '这里还没有内容'}
          description={
            filter === 'pending'
              ? '同步一个 Skill 源之后，新拉下来的条目会出现在这里等待审核。'
              : '换个筛选条件看看，或者先同步一个 Skill 源。'
          }
        />
      )}

      {items.length > 0 && (
        <>
          <div className="flex flex-wrap items-center gap-space-3">
            <label className="flex cursor-pointer items-center gap-space-2">
              <Checkbox checked={allSelected} onCheckedChange={toggleAll} />
              <span className="text-body-sm text-ink-700">
                {selected.size > 0 ? `已选 ${selected.size} 个` : `全选（${items.length}）`}
              </span>
            </label>
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="按名称 / slug / 简介筛选"
              className="h-9 max-w-[260px]"
            />
            <div className="ml-auto flex items-center gap-space-2">
              <Button
                size="sm"
                disabled={selected.size === 0 || reviewMutation.isPending}
                onClick={() => reviewMutation.mutate({ status: 'approved', targets: selectedItems })}
              >
                <Check className="mr-1 size-3.5" aria-hidden />
                通过
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={selected.size === 0 || reviewMutation.isPending}
                onClick={() => reviewMutation.mutate({ status: 'rejected', targets: selectedItems })}
              >
                <X className="mr-1 size-3.5" aria-hidden />
                驳回
              </Button>
            </div>
          </div>

          <ul className="overflow-hidden rounded-lg border border-border bg-surface">
            {items.map((s) => {
              const meta = STATUS_META[s.review_status as ReviewStatus] ?? STATUS_META.pending
              return (
                <li
                  key={keyOf(s)}
                  className="flex items-start gap-space-3 border-b border-border px-space-4 py-space-3 last:border-0"
                >
                  <Checkbox
                    className="mt-1"
                    checked={selected.has(keyOf(s))}
                    onCheckedChange={() => toggle(s)}
                    aria-label={`选择 ${s.name}`}
                  />
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex flex-wrap items-center gap-space-2">
                      <span className="text-body-sm font-medium text-ink-900">{s.name}</span>
                      <span className="text-ref text-caption text-ink-500">{s.slug}</span>
                      <span className={cn('text-caption rounded-full px-space-2 py-0.5', meta.className)}>{meta.label}</span>
                    </span>
                    <span className="text-body-sm line-clamp-2 text-ink-500">{s.summary || '上游没有提供简介。'}</span>
                    <span className="text-caption flex flex-wrap items-center gap-space-3 text-ink-500">
                      <span>来自 {s.source_name}</span>
                      {s.version && <span className="tabular">v{s.version}</span>}
                      <span className="tabular">★ {s.stars}</span>
                      <span className="tabular">↓ {s.downloads}</span>
                      {s.review_note && <span className="text-rust">备注：{s.review_note}</span>}
                    </span>
                  </div>
                  {/* 审核要看得到东西才能审——直接外链上游页面看完整内容。 */}
                  <a
                    href={`${s.source_base_url}/skills/${s.slug}`}
                    target="_blank"
                    rel="noreferrer noopener"
                    className="text-body-sm inline-flex shrink-0 items-center gap-1 text-blueprint hover:underline"
                  >
                    查看
                    <ExternalLink className="size-3" aria-hidden />
                  </a>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </Section>
  )
}
