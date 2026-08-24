import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { FilterChip, FilterChips } from '@/components/common/Page'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorPanel } from '@/components/common/EmptyState'
import { ListingCard } from '@/components/marketplace/ListingCard'
import { SubscribeDialog } from '@/components/marketplace/SubscribeDialog'
import { apiClient, unwrap } from '@/lib/api/client'
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

function CardSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="h-52 animate-pulse rounded-lg border border-border bg-surface-muted" />
      ))}
    </div>
  )
}

export function MarketplaceBrowsePage() {
  const [type, setType] = useState<ListingResourceType | 'all'>('all')
  const [search, setSearch] = useState('')
  const [subscribeTarget, setSubscribeTarget] = useState<ListingSummary | null>(null)
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['marketplace-listings', type, search],
    queryFn: async () =>
      unwrap<{ items: ListingSummary[]; has_more: boolean }>(
        await apiClient.GET('/marketplace/listings', {
          params: { query: { resource_type: type === 'all' ? undefined : type, q: search || undefined } },
        }),
      ),
  })

  const items = query.data?.items ?? []
  const isFiltered = type !== 'all' || search !== ''

  return (
    <div className="flex flex-col gap-space-7">
      <div className="flex flex-wrap items-center justify-between gap-space-4">
        <FilterChips>
          {TYPES.map((t) => (
            <FilterChip key={t.value} active={t.value === type} onClick={() => setType(t.value)}>
              {t.label}
            </FilterChip>
          ))}
        </FilterChips>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索名称或描述"
          className="w-full max-w-[240px]"
        />
      </div>

      {query.isLoading && <CardSkeleton />}
      {query.isError && <ErrorPanel message="广场列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && !isFiltered && (
        <EmptyState
          title="广场上还没有人发布东西"
          description="把你做好的 Bundle 或 Agent 发布出来，别人订阅后可以直接运行，但看不到你怎么编排的。"
          action={
            <Button size="sm" asChild>
              <Link to="/marketplace?tab=publish">发布我的第一个资源</Link>
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

      {items.length > 0 && (
        <div className="grid grid-cols-1 gap-space-5 md:grid-cols-2 xl:grid-cols-3">
          {items.map((listing) => (
            <ListingCard key={listing.id} listing={listing} onSubscribe={setSubscribeTarget} />
          ))}
        </div>
      )}

      <SubscribeDialog
        listing={subscribeTarget}
        open={!!subscribeTarget}
        onOpenChange={(v) => !v && setSubscribeTarget(null)}
        onSubscribed={() => {
          toast.success('已订阅')
          queryClient.invalidateQueries({ queryKey: ['marketplace-listings'] })
        }}
      />
    </div>
  )
}
