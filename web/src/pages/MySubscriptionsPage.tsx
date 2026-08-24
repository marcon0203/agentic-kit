import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { UpgradeDialog } from '@/components/marketplace/UpgradeDialog'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Subscription = components['schemas']['Subscription']

export function MySubscriptionsPage() {
  const queryClient = useQueryClient()
  const [upgradeTarget, setUpgradeTarget] = useState<Subscription | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['my-subscriptions'],
    queryFn: async () =>
      unwrap<{ items: Subscription[] }>(await apiClient.GET('/marketplace/subscriptions', {})),
  })

  async function unsubscribe(id: string) {
    setActionError(null)
    try {
      assertOk(await apiClient.DELETE('/marketplace/subscriptions/{id}', { params: { path: { id } } }))
      toast.success('已退订')
      queryClient.invalidateQueries({ queryKey: ['my-subscriptions'] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '退订失败')
    }
  }

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-6">
      {query.isLoading && <ListSkeleton />}
      {query.isError && <ErrorPanel message="订阅列表加载失败" onRetry={() => query.refetch()} />}
      {actionError && (
        <p role="alert" className="text-body-sm text-rust">
          {actionError}
        </p>
      )}

      {query.isSuccess && items.length === 0 && (
        <EmptyState
          title="还没有订阅任何资源"
          description="去广场看看有没有能直接拿来用的 Bundle 或 Agent。"
          action={
            <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" asChild>
              <Link to="/marketplace">去广场看看</Link>
            </Button>
          }
        />
      )}

      {items.length > 0 && (
        <ul className="flex flex-col gap-space-2">
          {items.map((sub) => {
            const hasNewVersion = sub.latest_version && sub.latest_version !== sub.subscribed_version
            return (
              <li key={sub.id} className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4">
                <div className="flex-1">
                  <div className="flex items-center gap-space-2">
                    <Link to={`/marketplace/listing/${sub.listing.listing_ref}`} className="text-ref-lg text-ink-900 hover:underline">
                      {sub.listing.display_meta.display_name}
                    </Link>
                    <span className="text-caption text-ink-500">v{sub.subscribed_version}</span>
                    {hasNewVersion && (
                      <span className="text-caption inline-flex items-center gap-1 rounded-xs border border-signal bg-signal-tint px-1.5 py-0.5 text-signal">
                        有新版本 v{sub.latest_version}
                      </span>
                    )}
                  </div>
                  <p className="text-body-sm text-ink-500">{sub.listing.author.display_name}</p>
                </div>
                {hasNewVersion && (
                  <Button size="sm" onClick={() => setUpgradeTarget(sub)}>
                    查看更新
                  </Button>
                )}
                <Button variant="ghost" size="sm" onClick={() => unsubscribe(sub.id)}>
                  退订
                </Button>
              </li>
            )
          })}
        </ul>
      )}

      <UpgradeDialog
        subscription={upgradeTarget}
        open={!!upgradeTarget}
        onOpenChange={(v) => !v && setUpgradeTarget(null)}
        onUpgraded={() => {
          toast.success('已升级')
          queryClient.invalidateQueries({ queryKey: ['my-subscriptions'] })
        }}
      />
    </div>
  )
}
