import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Check, ExternalLink, Globe, RefreshCw, X } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type SkillSource = components['schemas']['SkillSource']
type MarketSkill = components['schemas']['MarketSkill']
type ReviewStatus = components['schemas']['SkillReviewStatus']

const STATUS_META: Record<ReviewStatus, { label: string; className: string }> = {
  pending: { label: '待审核', className: 'bg-signal-tint text-signal' },
  approved: { label: '已通过', className: 'bg-moss-tint text-moss' },
  rejected: { label: '已驳回', className: 'bg-rust-tint text-rust' },
}

type Filter = ReviewStatus | 'all'

function formatSyncedAt(iso: string | null): string {
  if (!iso) return '从未同步'
  const d = new Date(iso)
  return `${d.toLocaleDateString()} ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

/**
 * 系统配置 → Skill 源 → 某个源的详情：这个源同步下来的 Skill 全在这里过
 * 一遍，只有通过的才会进用户侧的「Skill 管理 → 市场」并允许安装。
 *
 * 审核放在源详情而不是源列表下方：条目本来就属于某个源，一个源动辄几百条，
 * 混在一个大列表里既找不着也审不完。逐条审和批量审都支持——批量应付一次同
 * 步涌进来的量，逐条应付"这条我得看看"。
 */
export function SkillSourceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const sourceId = Number(id)

  const [filter, setFilter] = useState<Filter>('pending')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const sourcesQuery = useQuery({
    queryKey: ['skill-sources'],
    queryFn: async () => unwrap<{ items: SkillSource[] }>(await apiClient.GET('/skill-sources', {})),
  })
  const source = sourcesQuery.data?.items.find((s) => Number(s.id) === sourceId)

  const skillsQuery = useQuery({
    queryKey: ['skill-review', sourceId, filter],
    queryFn: async () =>
      unwrap<{ items: MarketSkill[]; counts: Record<ReviewStatus, number> }>(
        await apiClient.GET('/skill-sources/skills', {
          params: {
            query: {
              source_id: String(sourceId),
              ...(filter === 'all' ? {} : { review_status: filter }),
            },
          },
        }),
      ),
    enabled: Number.isFinite(sourceId),
  })

  const items = useMemo(() => {
    const all = skillsQuery.data?.items ?? []
    const q = search.trim().toLowerCase()
    if (!q) return all
    return all.filter(
      (s) =>
        s.slug.toLowerCase().includes(q) ||
        s.name.toLowerCase().includes(q) ||
        (s.summary ?? '').toLowerCase().includes(q),
    )
  }, [skillsQuery.data, search])

  const counts = skillsQuery.data?.counts
  const allSelected = items.length > 0 && items.every((s) => selected.has(s.slug))

  const reviewMutation = useMutation({
    mutationFn: async ({ status, targets }: { status: ReviewStatus; targets: MarketSkill[] }) =>
      unwrap<{ reviewed: number }>(
        await apiClient.POST('/skill-sources/skills/review', {
          body: { status, items: targets.map((s) => ({ source_id: sourceId, slug: s.slug })) },
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

  const syncMutation = useMutation({
    mutationFn: async () => unwrap<SkillSource>(await apiClient.POST('/skill-sources/{id}/sync', {
      params: { path: { id: String(sourceId) } },
    })),
    onSuccess: (src) => {
      toast.success(src.last_sync_error ? `同步完成，但有错误：${src.last_sync_error}` : '同步完成')
      queryClient.invalidateQueries({ queryKey: ['skill-sources'] })
      queryClient.invalidateQueries({ queryKey: ['skill-review'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '同步失败'),
  })

  function toggle(slug: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(slug)) next.delete(slug)
      else next.add(slug)
      return next
    })
  }

  const selectedItems = items.filter((s) => selected.has(s.slug))

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex flex-wrap items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/settings/skill-sources')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-md bg-surface-muted text-ink-500">
          <Globe className="size-4" />
        </span>
        <div className="flex min-w-0 flex-col">
          <span className="text-label-md text-ink-900">{source?.name ?? `源 #${sourceId}`}</span>
          {source && (
            <span className="text-caption flex flex-wrap items-center gap-space-3 text-ink-500">
              <a href={source.base_url} target="_blank" rel="noreferrer" className="text-blueprint hover:underline">
                {source.base_url}
              </a>
              <span>{formatSyncedAt(source.last_synced_at ?? null)}</span>
            </span>
          )}
        </div>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto"
          disabled={syncMutation.isPending}
          onClick={() => syncMutation.mutate()}
        >
          <RefreshCw className={cn('mr-1 size-3.5', syncMutation.isPending && 'animate-spin')} aria-hidden />
          同步
        </Button>
      </div>

      {source?.last_sync_error && (
        <p role="alert" className="text-body-sm rounded-md border border-rust px-space-4 py-space-2 text-rust">
          上次同步失败：{source.last_sync_error}
        </p>
      )}

      <p className="text-body-sm text-ink-500">
        同步进来的 Skill 默认待审核，只有通过审核的才会出现在「Skill 管理 → 市场」里供用户安装。
      </p>

      <div className="flex flex-wrap items-center gap-space-2">
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

      {skillsQuery.isLoading && <ListSkeleton />}
      {skillsQuery.isError && <ErrorPanel message="Skill 列表没能加载出来" onRetry={() => skillsQuery.refetch()} />}

      {skillsQuery.isSuccess && items.length === 0 && (
        <EmptyRail
          title={filter === 'pending' ? '没有待审核的 Skill' : '这里还没有内容'}
          description={
            filter === 'pending'
              ? '这个源同步下来的条目都已经审过了。点右上角「同步」拉取上游的最新内容。'
              : '换个筛选条件看看，或者先同步一次。'
          }
        />
      )}

      {items.length > 0 && (
        <>
          <div className="flex flex-wrap items-center gap-space-3">
            <label className="flex cursor-pointer items-center gap-space-2">
              <Checkbox
                checked={allSelected}
                onCheckedChange={() => setSelected(allSelected ? new Set() : new Set(items.map((s) => s.slug)))}
              />
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
                批量通过
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={selected.size === 0 || reviewMutation.isPending}
                onClick={() => reviewMutation.mutate({ status: 'rejected', targets: selectedItems })}
              >
                <X className="mr-1 size-3.5" aria-hidden />
                批量驳回
              </Button>
            </div>
          </div>

          <ul className="overflow-hidden rounded-lg border border-border bg-surface">
            {items.map((s) => {
              const meta = STATUS_META[s.review_status as ReviewStatus] ?? STATUS_META.pending
              return (
                <li
                  key={s.slug}
                  className="flex items-start gap-space-3 border-b border-border px-space-4 py-space-3 last:border-0"
                >
                  <Checkbox
                    className="mt-1"
                    checked={selected.has(s.slug)}
                    onCheckedChange={() => toggle(s.slug)}
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
                      {s.version && <span className="tabular">v{s.version}</span>}
                      <span className="tabular">★ {s.stars}</span>
                      <span className="tabular">↓ {s.downloads}</span>
                      {/* 审核要看得到东西才能审——直接外链上游页面看完整内容。 */}
                      <a
                        href={`${s.source_base_url}/skills/${s.slug}`}
                        target="_blank"
                        rel="noreferrer noopener"
                        className="inline-flex items-center gap-1 text-blueprint hover:underline"
                      >
                        查看上游
                        <ExternalLink className="size-3" aria-hidden />
                      </a>
                      {s.review_note && <span className="text-rust">备注：{s.review_note}</span>}
                    </span>
                  </div>

                  {/* 逐条审：不用先勾选再点批量，一条一条看的时候这才是顺手的动作。 */}
                  <div className="flex shrink-0 items-center gap-space-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`通过 ${s.name}`}
                      disabled={s.review_status === 'approved' || reviewMutation.isPending}
                      className="text-ink-500 hover:text-moss"
                      onClick={() => reviewMutation.mutate({ status: 'approved', targets: [s] })}
                    >
                      <Check className="size-4" aria-hidden />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`驳回 ${s.name}`}
                      disabled={s.review_status === 'rejected' || reviewMutation.isPending}
                      className="text-ink-500 hover:text-rust"
                      onClick={() => reviewMutation.mutate({ status: 'rejected', targets: [s] })}
                    >
                      <X className="size-4" aria-hidden />
                    </Button>
                  </div>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </div>
  )
}
