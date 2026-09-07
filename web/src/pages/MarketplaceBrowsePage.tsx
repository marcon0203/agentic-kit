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

  // 类型筛选改在客户端做，服务端只按搜索词取一次。这样分面上的计数和筛选
  // 后看到的条数永远是同一批数据算出来的——如果类型也交给服务端过滤，选中
  // 「Bundle」之后就只剩 Bundle 的数据，其它分面的计数便无从算起，只能编。
  const query = useQuery({
    queryKey: ['marketplace-listings', search],
    queryFn: async () =>
      unwrap<{ items: ListingSummary[]; has_more: boolean }>(
        await apiClient.GET('/marketplace/listings', { params: { query: { q: search || undefined } } }),
      ),
  })

  const loaded = query.data?.items ?? []
  const hasMore = query.data?.has_more ?? false
  const items = browseTab === 'all' && type !== 'all' ? loaded.filter((l) => l.resource_type === type) : loaded
  const isFiltered = (browseTab === 'all' && type !== 'all') || search !== ''

  // 计数只统计这一页真的拿到的条目。后端还有下一页时补一个「+」，
  // 而不是把"这一页有 12 条"说成"一共 12 条"。
  const counts = useMemo(() => {
    const by = new Map<ListingResourceType | 'all', number>([['all', loaded.length]])
    for (const l of loaded) by.set(l.resource_type, (by.get(l.resource_type) ?? 0) + 1)
    return by
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 同下：query.data 才是真依赖
  }, [query.data])

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
      {/* 区块头：名字 + 真实计数 chip 在左，控件收在同一行的右侧。计数用的
          是这次实际返回的条数，不是编出来的总量。 */}
      <div className="flex flex-wrap items-center justify-between gap-space-4">
        <div className="flex items-center gap-space-3">
          <h2 className="text-display-md text-ink-900">应用广场</h2>
          {query.isSuccess && (
            <span className="text-caption tabular rounded-full bg-surface-muted px-space-3 py-0.5 text-ink-700">
              {items.length}
              {hasMore && '+'} 个
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-space-3">
          <div role="tablist" className="flex items-center gap-space-1 rounded-full border border-border bg-surface-muted p-1">
            {(['featured', 'all'] as const).map((t) => (
              <button
                key={t}
                type="button"
                role="tab"
                aria-selected={browseTab === t}
                onClick={() => setBrowseTab(t)}
                className={cn(
                  'text-body-sm whitespace-nowrap rounded-full px-space-4 py-1.5 transition-colors duration-150 ease-out',
                  browseTab === t ? 'bg-surface text-ink-900 shadow-sm' : 'text-ink-500 hover:text-ink-900',
                )}
              >
                {t === 'featured' ? '精选应用' : '全部应用'}
              </button>
            ))}
          </div>
          {/* 不能用 w-full：在这个会换行的 flex 行里它会索取整行宽度，把自己
              挤到 tab 组的下一行去。给一个固定宽度，窄屏时再让它整行。 */}
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="搜索名称或描述"
            className="w-[240px] max-w-full"
          />
        </div>
      </div>

      {/* 这是一句解释性说明，不是营销位——用应用内的扁平卡片语言承载（描边
          分层 + 图标 chip），而不是整块彩色渐变：控制台页面里的渐变只会把
          一句注释喊成横幅。 */}
      {browseTab === 'featured' && (
        <div className="flex items-start gap-space-4 rounded-lg border border-border bg-surface p-space-5">
          <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-sm bg-blueprint-tint text-blueprint">
            <Sparkles className="size-5" />
          </span>
          <div className="flex flex-col gap-1">
            <p className="text-label-md text-ink-900">订阅即用，作者的编排图与提示词不会带出来</p>
            <p className="text-body-sm text-ink-700">订阅锁定版本；有新版本发布时会单独提醒你升级。</p>
          </div>
        </div>
      )}

      {/* 带计数的分面。参考站把它做成一整条左侧栏，这里没有照搬：这个页面
          外面已经有 AppsLayout 的二级菜单栏，再加一列就是双左栏。计数这个
          真正有用的部分留下，形态收成一行。 */}
      {browseTab === 'all' && (
        <FilterChips>
          {TYPES.map((t) => {
            const n = counts.get(t.value) ?? 0
            return (
              <FilterChip key={t.value} active={t.value === type} onClick={() => setType(t.value)}>
                {t.label}
                {/* 计数继承 chip 自身的颜色再压低透明度，而不是写死一个灰
                    ——选中态的 chip 是紫底，写死的 ink-500 在上面只有 2:1。 */}
                <span className="tabular ml-space-2 opacity-60">{n}</span>
              </FilterChip>
            )
          })}
        </FilterChips>
      )}

      {query.isLoading && <CardSkeleton />}
      {query.isError && <ErrorPanel message="广场列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && !isFiltered && (
        <EmptyState
          title="广场上还没有人发布东西"
          description="把你做好的 Bundle 或 Agent 发布出来，别人订阅后可以直接运行，但看不到你怎么编排的。"
          action={
            <Button size="sm" asChild>
              <Link to="/apps/publish">发布我的第一个资源</Link>
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
