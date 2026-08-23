import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Lock, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { SubscribeDialog } from '@/components/marketplace/SubscribeDialog'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingDetail = components['schemas']['ListingDetail']

export function ListingDetailPage() {
  const { ref } = useParams<{ ref: string }>()
  const [subscribeOpen, setSubscribeOpen] = useState(false)
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['listing', ref],
    queryFn: async () =>
      unwrap<ListingDetail>(await apiClient.GET('/marketplace/listings/{ref}', { params: { path: { ref: ref! } } })),
    enabled: !!ref,
  })

  if (query.isLoading) return <ListSkeleton rows={8} />
  if (query.isError) return <ErrorPanel message="资源不存在或已下架" onRetry={() => query.refetch()} />
  const listing = query.data
  if (!listing) return null

  const constraints = listing.constraints_summary

  return (
    <div className="flex flex-col gap-space-6">
      <div className="grid grid-cols-1 gap-space-8 lg:grid-cols-[1.08fr_.92fr]">
        <div className="flex flex-col gap-space-6">
          <div>
            <div className="flex items-center gap-space-3">
              <Badge variant="outline">{listing.resource_type}</Badge>
              <span className="text-caption inline-flex items-center gap-1 text-ink-700">
                <Lock className="size-3" aria-hidden />
                黑盒
              </span>
            </div>
            <h1 className="text-headline-lg mt-space-2 text-ink-900">{listing.display_meta.display_name}</h1>
            <p className="text-body-sm mt-space-2 text-ink-500">
              @{listing.author.display_name} · v{listing.version} · {listing.subscriber_count} 人订阅 · 运行{' '}
              {listing.run_count} 次
            </p>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-headline-sm">简介</CardTitle>
            </CardHeader>
            <CardContent className="text-body-md text-ink-700">{listing.display_meta.description}</CardContent>
          </Card>

          {listing.display_meta.usage && (
            <Card>
              <CardHeader>
                <CardTitle className="text-headline-sm">怎么用</CardTitle>
              </CardHeader>
              <CardContent className="text-body-md whitespace-pre-wrap text-ink-700">
                {listing.display_meta.usage}
              </CardContent>
            </Card>
          )}

          {listing.display_meta.io_description && (
            <Card>
              <CardHeader>
                <CardTitle className="text-headline-sm">输入输出</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-space-2 text-body-md text-ink-700">
                {listing.display_meta.io_description.input && <p>输入：{listing.display_meta.io_description.input}</p>}
                {listing.display_meta.io_description.outputs && listing.display_meta.io_description.outputs.length > 0 && (
                  <p>输出字段：{listing.display_meta.io_description.outputs.join('、')}</p>
                )}
              </CardContent>
            </Card>
          )}

          <div className="flex items-start gap-space-3 rounded-md border border-border bg-surface-muted px-space-5 py-space-4">
            <Lock className="mt-0.5 size-4 shrink-0 text-ink-700" aria-hidden />
            <p className="text-body-sm text-ink-700">该资源为黑盒发布，内部编排结构与提示词不公开。</p>
          </div>
        </div>

        <div className="flex flex-col gap-space-5 self-start lg:sticky lg:top-space-6">
          <Card>
            <CardContent className="pt-space-6">
              {listing.subscribed ? (
                <Badge className="w-full justify-center py-space-2">已订阅</Badge>
              ) : (
                <button
                  type="button"
                  onClick={() => setSubscribeOpen(true)}
                  className="h-11 w-full rounded-full bg-[image:var(--gradient-brand)] text-body-md font-medium text-white shadow-sm hover:brightness-[1.04]"
                >
                  订阅 v{listing.version}
                </button>
              )}
            </CardContent>
          </Card>

          {constraints && (
            <Card>
              <CardHeader>
                <CardTitle className="text-headline-sm flex items-center gap-space-2">
                  <TriangleAlert className="size-4 text-[var(--color-warning)]" aria-hidden />
                  执行约束
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-space-2 text-body-sm text-ink-700">
                {constraints.max_tool_calls != null && <p>最大工具调用数：{constraints.max_tool_calls}</p>}
                {constraints.timeout_seconds != null && <p>超时：{constraints.timeout_seconds}s</p>}
                {constraints.estimated_tokens_range && <p>预计 Token：{constraints.estimated_tokens_range}</p>}
                <p className="text-caption text-ink-500">用于预估运行成本</p>
              </CardContent>
            </Card>
          )}

          {listing.versions && listing.versions.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-headline-sm">版本历史</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-space-4">
                {listing.versions.map((v) => (
                  <div key={v.version} className="border-b border-border pb-space-3 last:border-0 last:pb-0">
                    <p className="text-label-md text-ink-900">v{v.version}</p>
                    {v.changelog && <p className="text-body-sm mt-space-1 text-ink-700">{v.changelog}</p>}
                  </div>
                ))}
              </CardContent>
            </Card>
          )}
        </div>
      </div>

      <SubscribeDialog
        listing={listing}
        open={subscribeOpen}
        onOpenChange={setSubscribeOpen}
        onSubscribed={() => {
          toast.success('已订阅')
          queryClient.invalidateQueries({ queryKey: ['listing', ref] })
        }}
      />
    </div>
  )
}
