import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { PublishForm } from '@/components/marketplace/PublishForm'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']

export function MyListingsPage() {
  const user = useAuthStore((s) => s.user)
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [unpublishError, setUnpublishError] = useState<string | null>(null)

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
      <div className="flex items-center justify-between">
        <h2 className="text-display-md text-ink-900">我的发布</h2>
        {!showForm && <Button onClick={() => setShowForm(true)}>发布新资源</Button>}
      </div>

      {showForm ? (
        <div className="flex flex-col gap-space-4">
          <Button variant="outline" size="sm" className="self-start" onClick={() => setShowForm(false)}>
            返回列表
          </Button>
          <PublishForm
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
              action={<Button size="sm" onClick={() => setShowForm(true)}>发布第一个资源</Button>}
            />
          )}

          {mine.length > 0 && (
            <ul className="flex flex-col gap-space-2">
              {mine.map((l) => (
                <li key={l.id} className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4">
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
                  <Button variant="outline" size="sm" onClick={() => unpublish(l)}>
                    停止分发
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
