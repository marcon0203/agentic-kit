import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ChevronDown, ChevronUp } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { PublishForm, type PublishFormInitial } from '@/components/marketplace/PublishForm'
import { ApiUsagePanel } from '@/components/marketplace/ApiUsagePanel'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']
type ListingResourceType = components['schemas']['ListingResourceType']

const RESOURCE_TYPES: readonly ListingResourceType[] = ['bundle', 'agent', 'skill', 'mcp']

/**
 * DependencyErrorPanel 的"去发布"链接带 ?type=&ref=&version= 跳过来——
 * 一进页面就该是展开的表单，而不是让人再点一次"发布新资源"。
 */
function initialFromQuery(params: URLSearchParams): PublishFormInitial | undefined {
  const type = params.get('type')
  const ref = params.get('ref')
  if (!type || !ref || !RESOURCE_TYPES.includes(type as ListingResourceType)) return undefined
  return { type: type as ListingResourceType, ref, version: params.get('version') ?? undefined }
}

export function MyListingsPage() {
  const user = useAuthStore((s) => s.user)
  const queryClient = useQueryClient()
  const [searchParams] = useSearchParams()
  const initial = initialFromQuery(searchParams)
  const [showForm, setShowForm] = useState(!!initial)
  const [unpublishError, setUnpublishError] = useState<string | null>(null)
  const [apiPanelFor, setApiPanelFor] = useState<string | null>(null)

  // No "mine" filter exists on GET /marketplace/listings — browse
  // everything and keep what this user authored. Fine for a personal
  // catalog's realistic size; won't paginate correctly past the first
  // page for someone with a very large publish history.
  const query = useQuery({
    queryKey: ['marketplace-listings', 'all-for-mine-filter'],
    queryFn: async () =>
      unwrap<{ items: ListingSummary[] }>(await apiClient.GET('/marketplace/listings', { params: { query: {} } })),
  })

  const mine = (query.data?.items ?? []).filter((l) => l.author.id === user?.id)

  async function unpublish(listing: ListingSummary) {
    setUnpublishError(null)
    try {
      assertOk(await apiClient.POST('/marketplace/listings/{id}/unpublish', { params: { path: { id: listing.id } } }))
      toast.success('已停止分发')
      queryClient.invalidateQueries({ queryKey: ['marketplace-listings'] })
    } catch (err) {
      if (err instanceof ApiError && err.code === 70007 && err.details) {
        setUnpublishError(`无法停止分发：${err.details.map((d) => d.reason).join('；')}`)
      } else {
        setUnpublishError(err instanceof ApiError ? err.message : '操作失败')
      }
    }
  }

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex items-center justify-end">
        {!showForm && <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setShowForm(true)}>发布新资源</Button>}
      </div>

      {showForm ? (
        <div className="flex flex-col gap-space-4">
          <Button variant="outline" size="sm" className="self-start" onClick={() => setShowForm(false)}>
            返回列表
          </Button>
          <PublishForm
            initial={initial}
            onPublished={() => {
              toast.success('已发布')
              queryClient.invalidateQueries({ queryKey: ['marketplace-listings'] })
              setShowForm(false)
            }}
          />
        </div>
      ) : (
        <>
          {unpublishError && (
            <p role="alert" className="text-body-sm text-rust">
              {unpublishError}
            </p>
          )}

          {query.isLoading && <ListSkeleton />}
          {query.isError && <ErrorPanel message="发布列表加载失败" onRetry={() => query.refetch()} />}

          {query.isSuccess && mine.length === 0 && (
            <EmptyState
              title="还没有发布任何资源"
              description="把自己的 Bundle 或 Agent 发布到广场，让其他人可以订阅使用。"
              action={<Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setShowForm(true)}>发布第一个资源</Button>}
            />
          )}

          {mine.length > 0 && (
            <ul className="flex flex-col gap-space-2">
              {mine.map((l) => {
                const apiOpen = apiPanelFor === l.id
                return (
                  <li key={l.id} className="rounded-md border border-border bg-surface">
                    <div className="flex items-center gap-space-4 px-space-5 py-space-4">
                      <div className="flex-1">
                        <div className="flex items-center gap-space-2">
                          <span className="text-body-md text-ink-900">{l.display_meta.display_name}</span>
                          <span className="text-caption text-ink-500">v{l.version}</span>
                          <Ref tone="blueprint">{l.resource_type}</Ref>
                        </div>
                        <p className="text-body-sm text-ink-500">
                          {l.subscriber_count} 人订阅 · 运行 {l.run_count} 次
                        </p>
                      </div>
                      {/* 只有 bundle 能被 POST /runs 直接调用——agent/skill/mcp
                          是别的 Bundle 的依赖件，本身不是一个可运行的调用
                          目标，这里的"调用方式"对它们没有意义。 */}
                      {l.resource_type === 'bundle' && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setApiPanelFor(apiOpen ? null : l.id)}
                        >
                          调用方式
                          {apiOpen ? <ChevronUp className="ml-1 size-4" aria-hidden /> : <ChevronDown className="ml-1 size-4" aria-hidden />}
                        </Button>
                      )}
                      <Button variant="outline" size="sm" onClick={() => unpublish(l)}>
                        停止分发
                      </Button>
                    </div>
                    {apiOpen && (
                      <div className="border-t border-border bg-surface-muted px-space-5 py-space-4">
                        <ApiUsagePanel listingRef={l.listing_ref} />
                      </div>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
