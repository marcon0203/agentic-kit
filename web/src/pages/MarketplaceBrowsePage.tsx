import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
        <div key={i} className="h-56 animate-pulse rounded-lg bg-surface-muted" />
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
      <div>
        <h1 className="text-headline-lg text-ink-900">应用广场</h1>
        <p className="text-body-lg mt-space-2 text-ink-700">订阅他人发布的 Bundle、Agent、Skill 或 MCP Server。</p>
      </div>

      <div className="flex flex-wrap items-center gap-space-4">
        <Tabs value={type} onValueChange={(v) => setType(v as typeof type)}>
          <TabsList>
            {TYPES.map((t) => (
              <TabsTrigger key={t.value} value={t.value}>
                {t.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索名称或描述"
          className="max-w-xs"
        />
      </div>

      {query.isLoading && <CardSkeleton />}
      {query.isError && <ErrorPanel message="广场列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && !isFiltered && (
        <EmptyState
          title="广场还没有内容"
          description="发布一个 Bundle 或 Agent，让其他人可以订阅使用。"
          action={
            <Button size="sm" asChild>
              <Link to="/marketplace?tab=publish">发布我的第一个资源</Link>
            </Button>
          }
        />
      )}

      {query.isSuccess && items.length === 0 && isFiltered && (
        <EmptyState
          title="没有找到相关资源"
          description="换一个关键词或类型试试。"
          action={
            <Button
              variant="secondary"
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
        <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2 lg:grid-cols-3">
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
