import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Check, ExternalLink, RefreshCw, Server, X } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyRail } from '@/components/common/Rail'
import { Pagination } from '@/components/common/Pagination'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MCPSource = components['schemas']['MCPSource']
type MarketMCPServer = components['schemas']['MarketMCPServer']
type ReviewStatus = components['schemas']['SkillReviewStatus']

const STATUS_META: Record<ReviewStatus, { label: string; className: string }> = {
  pending: { label: '待审核', className: 'bg-signal-tint text-signal' },
  approved: { label: '已通过', className: 'bg-moss-tint text-moss' },
  rejected: { label: '已驳回', className: 'bg-rust-tint text-rust' },
}

type Filter = ReviewStatus | 'all'

/** 一屏审 15 条：再多就得一直滚，批量勾选也失去"这一批"的边界感。 */
const PAGE_SIZE = 15

/** 搜索词防抖：输入停下来再发请求，否则每个字符一次往返。 */
function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), delayMs)
    return () => clearTimeout(t)
  }, [value, delayMs])
  return settled
}

function formatSyncedAt(iso: string | null): string {
  if (!iso) return '从未同步'
  const d = new Date(iso)
  return `${d.toLocaleDateString()} ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

/**
 * 系统配置 → MCP 源 → 某个源的详情：这个源同步下来的 Server 全在这里过一
 * 遍，只有通过的才会进用户侧的「MCP 管理 → 市场」并允许接入。
 *
 * MCP 的审核比 Skill 更该有：过审等于允许本部署的用户把请求发到那个第三方
 * 地址上去。所以列表把远端地址直接摆出来，而不是藏在详情页里。
 */
export function McpSourceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const sourceId = Number(id)

  const [filter, setFilter] = useState<Filter>('pending')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [page, setPage] = useState(1)

  const sourcesQuery = useQuery({
    queryKey: ['mcp-sources'],
    queryFn: async () => unwrap<{ items: MCPSource[] }>(await apiClient.GET('/mcp-sources', {})),
  })
  const source = sourcesQuery.data?.items.find((s) => Number(s.id) === sourceId)

  const debouncedSearch = useDebounced(search, 300)

  // 筛选条件或搜索词一变，当前页码多半越界，回第一页。
  useEffect(() => {
    setPage(1)
  }, [filter, debouncedSearch, sourceId])

  const serversQuery = useQuery({
    queryKey: ['mcp-review', sourceId, filter, debouncedSearch, page],
    queryFn: async () =>
      unwrap<{
        items: MarketMCPServer[]
        total: number
        counts: Record<ReviewStatus, number>
      }>(
        await apiClient.GET('/mcp-sources/servers', {
          params: {
            query: {
              source_id: String(sourceId),
              ...(filter === 'all' ? {} : { review_status: filter }),
              ...(debouncedSearch ? { q: debouncedSearch } : {}),
              page,
              page_size: PAGE_SIZE,
            },
          },
        }),
      ),
    enabled: Number.isFinite(sourceId),
    // 翻页时保留上一页的数据，列表不会闪成骨架屏再闪回来。
    placeholderData: (prev) => prev,
  })

  const visible = serversQuery.data?.items ?? []
  const total = serversQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // 审核完一批之后列表会变短，当前页可能落到末页之后——退回去，免得停在
  // 一张空页上。
  useEffect(() => {
    if (page > pageCount) setPage(pageCount)
  }, [page, pageCount])

  const counts = serversQuery.data?.counts
  // 全选只覆盖当前页：跨页的"全选"点下去会连没看过的条目一起批掉。
  const allSelected = visible.length > 0 && visible.every((s) => selected.has(Number(s.id)))

  const reviewMutation = useMutation({
    mutationFn: async ({ status, targets }: { status: ReviewStatus; targets: MarketMCPServer[] }) =>
      unwrap<{ reviewed: number }>(
        await apiClient.POST('/mcp-sources/servers/review', {
          body: { status, ids: targets.map((s) => Number(s.id)) },
        }),
      ),
    onSuccess: (res, vars) => {
      toast.success(`${vars.status === 'approved' ? '已通过' : '已驳回'} ${res.reviewed} 个 Server`)
      setSelected(new Set())
      queryClient.invalidateQueries({ queryKey: ['mcp-review'] })
      // 市场视图只显示通过的条目，审核结果要立刻反映过去。
      queryClient.invalidateQueries({ queryKey: ['mcp-market'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '审核没能完成，请再试一次'),
  })

  const syncMutation = useMutation({
    mutationFn: async () =>
      unwrap<MCPSource>(
        await apiClient.POST('/mcp-sources/{id}/sync', { params: { path: { id: String(sourceId) } } }),
      ),
    onSuccess: (src) => {
      toast.success(src.last_sync_error ? `同步完成，但有错误：${src.last_sync_error}` : '同步完成')
      queryClient.invalidateQueries({ queryKey: ['mcp-sources'] })
      queryClient.invalidateQueries({ queryKey: ['mcp-review'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '同步失败'),
  })

  function toggle(rowId: number) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(rowId)) next.delete(rowId)
      else next.add(rowId)
      return next
    })
  }

  const selectedItems = visible.filter((s) => selected.has(Number(s.id)))

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex flex-wrap items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/settings/mcp-sources')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span
          aria-hidden
          className="flex size-9 shrink-0 items-center justify-center rounded-md bg-surface-muted text-ink-500"
        >
          <Server className="size-4" />
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
        同步进来的 Server 默认待审核。通过审核等于允许本部署的用户把请求发到那个第三方地址上，先看清楚地址再放行。
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

        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="按名称 / 限定名 / 简介搜索"
          className="ml-auto h-9 max-w-[260px]"
        />
      </div>

      {serversQuery.isLoading && <ListSkeleton />}
      {serversQuery.isError && <ErrorPanel message="Server 列表没能加载出来" onRetry={() => serversQuery.refetch()} />}

      {serversQuery.isSuccess && visible.length === 0 && (
        <EmptyRail
          title={debouncedSearch ? '没有匹配的 Server' : filter === 'pending' ? '没有待审核的 Server' : '这里还没有内容'}
          description={
            debouncedSearch
              ? `当前筛选下没有匹配「${debouncedSearch}」的条目，换个词或切到「全部」看看。`
              : filter === 'pending'
                ? '这个源同步下来的条目都已经审过了。点右上角「同步」拉取上游的最新内容。'
                : '换个筛选条件看看，或者先同步一次。'
          }
        />
      )}

      {visible.length > 0 && (
        <>
          <div className="flex flex-wrap items-center gap-space-3">
            <label className="flex cursor-pointer items-center gap-space-2">
              <Checkbox
                checked={allSelected}
                onCheckedChange={() =>
                  setSelected(allSelected ? new Set() : new Set(visible.map((s) => Number(s.id))))
                }
              />
              <span className="text-body-sm text-ink-700">
                {selected.size > 0 ? `已选 ${selected.size} 个` : `全选本页（${visible.length}）`}
              </span>
            </label>
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
            {visible.map((s) => {
              const meta = STATUS_META[s.review_status as ReviewStatus] ?? STATUS_META.pending
              return (
                <li
                  key={s.id}
                  className="flex items-start gap-space-3 border-b border-border px-space-4 py-space-3 last:border-0"
                >
                  <Checkbox
                    className="mt-1"
                    checked={selected.has(Number(s.id))}
                    onCheckedChange={() => toggle(Number(s.id))}
                    aria-label={`选择 ${s.name}`}
                  />
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex flex-wrap items-center gap-space-2">
                      <span className="text-ref text-body-sm font-medium text-ink-900">{s.slug}</span>
                      <span className={cn('text-caption rounded-full px-space-2 py-0.5', meta.className)}>
                        {meta.label}
                      </span>
                      {!s.installable && (
                        <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">
                          仅本地运行
                        </span>
                      )}
                    </span>
                    <span className="text-body-sm line-clamp-2 text-ink-500">{s.summary || '上游没有提供简介。'}</span>
                    {/* 要审的核心信息就是这个地址：过审等于放行本部署的用户
                        往它上面发请求。所以摆在列表里，不藏在详情后面。 */}
                    {s.remote_url && (
                      <span className="text-ref text-caption truncate text-ink-700">{s.remote_url}</span>
                    )}
                    <span className="text-caption flex flex-wrap items-center gap-space-3 text-ink-500">
                      {s.version && <span className="tabular">v{s.version}</span>}
                      {s.remote_type && <span>{s.remote_type}</span>}
                      {s.repository_url && (
                        <a
                          href={s.repository_url}
                          target="_blank"
                          rel="noreferrer noopener"
                          className="inline-flex items-center gap-1 text-blueprint hover:underline"
                        >
                          查看源码
                          <ExternalLink className="size-3" aria-hidden />
                        </a>
                      )}
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

          <div className="flex flex-wrap items-center justify-between gap-space-3">
            <span className="text-caption text-ink-500">
              共 {total} 个，第 {page} / {pageCount} 页
            </span>
            <Pagination
              page={page}
              pageCount={pageCount}
              pageSize={PAGE_SIZE}
              onPageChange={(p) => {
                setPage(p)
                // 勾选是"这一页这一批"的意思，翻页就作废，免得批量误伤别页。
                setSelected(new Set())
              }}
            />
          </div>
        </>
      )}
    </div>
  )
}
