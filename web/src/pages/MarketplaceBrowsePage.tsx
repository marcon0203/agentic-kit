import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Sparkles } from 'lucide-react'

import { FilterChip, FilterChips } from '@/components/common/Page'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorPanel } from '@/components/common/EmptyState'
import { AppCard } from '@/components/marketplace/AppCard'
import { apiClient, unwrap } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type ListingResourceType = components['schemas']['ListingResourceType']
type ListingSummary = components['schemas']['ListingSummary']

const TYPES: { value: ListingResourceType | 'all'; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'bundle', label: 'Bundle' },
  { value: 'agent', label: 'Agent' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp', label: 'MCP' },
]

const CATEGORIES: { value: ListingResourceType; label: string }[] = [
  { value: 'bundle', label: '编排应用 · Bundle' },
  { value: 'agent', label: '单体应用 · Agent' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp', label: 'MCP Server' },
]

function CardSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-space-4 md:grid-cols-2 xl:grid-cols-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="h-24 animate-pulse rounded-lg border border-border bg-surface-muted" />
      ))}
    </div>
  )
}

export function MarketplaceBrowsePage() {
  const [browseTab, setBrowseTab] = useState<'featured' | 'all'>('featured')
  const [type, setType] = useState<ListingResourceType | 'all'>('all')
  const [search, setSearch] = useState('')

  const query = useQuery({
    queryKey: ['marketplace-listings', browseTab === 'all' ? type : 'all', search],
    queryFn: async () =>
      unwrap<{ items: ListingSummary[]; has_more: boolean }>(
        await apiClient.GET('/marketplace/listings', {
          params: {
            query: {
              resource_type: browseTab === 'all' && type !== 'all' ? type : undefined,
              q: search || undefined,
            },
          },
        }),
      ),
  })

  const items = query.data?.items ?? []
  const isFiltered = (browseTab === 'all' && type !== 'all') || search !== ''

  const grouped = useMemo(() => {
    const byType = new Map<ListingResourceType, ListingSummary[]>()
    for (const item of items) {
      const list = byType.get(item.resource_type) ?? []
      list.push(item)
      byType.set(item.resource_type, list)
    }
    return CATEGORIES.map((c) => ({ ...c, items: byType.get(c.value) ?? [] })).filter(
      (c) => c.items.length > 0,
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `items` is a fresh [] on every render when data is empty; query.data is the real dependency
  }, [query.data])

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex flex-wrap items-center justify-between gap-space-4">
        <div role="tablist" className="flex items-center gap-space-1 rounded-full border border-border bg-surface-muted p-1">
          {(['featured', 'all'] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={browseTab === t}
              onClick={() => setBrowseTab(t)}
              className={cn(
                'text-body-sm rounded-full px-space-4 py-1.5 transition-colors',
                browseTab === t ? 'bg-surface text-ink-900 shadow-sm' : 'text-ink-500 hover:text-ink-900',
              )}
            >
              {t === 'featured' ? '精选应用' : '全部应用'}
            </button>
          ))}
        </div>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索名称或描述"
          className="w-full max-w-[240px]"
        />
      </div>

      {browseTab === 'featured' && (
        <div className="flex items-center gap-space-4 overflow-hidden rounded-lg bg-gradient-cta p-space-6 text-white">
          <Sparkles className="size-8 shrink-0" aria-hidden />
          <div className="flex flex-col gap-1">
            <p className="text-display-sm">订阅即用，作者的编排图与提示词不会带出来</p>
            <p className="text-body-sm text-white/80">订阅锁定版本；有新版本发布时会单独提醒你升级。</p>
          </div>
        </div>
      )}

      {browseTab === 'all' && (
        <FilterChips>
          {TYPES.map((t) => (
            <FilterChip key={t.value} active={t.value === type} onClick={() => setType(t.value)}>
              {t.label}
            </FilterChip>
          ))}
        </FilterChips>
      )}

      {query.isLoading && <CardSkeleton />}
      {query.isError && <ErrorPanel message="广场列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && !isFiltered && (
        <EmptyState
          title="广场上还没有人发布东西"
          description="把你做好的 Bundle 或 Agent 发布出来，别人订阅后可以直接运行，但看不到你怎么编排的。"
          action={
            <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" asChild>
              <Link to="/apps?tab=publish">发布我的第一个资源</Link>
            </Button>
          }
        />
      )}

      {query.isSuccess && items.length === 0 && isFiltered && (
        <EmptyState
          title="没有匹配的资源"
          description="搜索只匹配名称和简介，不搜索资源内部内容——广场上的资源本来就是黑盒。"
          action={
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setType('all')
                setSearch('')
              }}
            >
              清除筛选
            </Button>
          }
        />
      )}

      {browseTab === 'featured' && grouped.length > 0 && (
        <div className="flex flex-col gap-space-6">
          {grouped.map((group) => (
            <div key={group.value} className="flex flex-col gap-space-3">
              <h3 className="text-label-md text-ink-900">{group.label}</h3>
              <div className="grid grid-cols-1 gap-space-3 md:grid-cols-2 xl:grid-cols-3">
                {group.items.map((listing) => (
                  <AppCard key={listing.id} listing={listing} />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {browseTab === 'all' && items.length > 0 && (
        <div className="grid grid-cols-1 gap-space-3 md:grid-cols-2 xl:grid-cols-3">
          {items.map((listing) => (
            <AppCard key={listing.id} listing={listing} />
          ))}
        </div>
      )}
    </div>
  )
}
