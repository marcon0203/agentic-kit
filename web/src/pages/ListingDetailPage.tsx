import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Flag, Lock, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Ref } from '@/components/common/Page'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { SubscribeDialog } from '@/components/marketplace/SubscribeDialog'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingDetail = components['schemas']['ListingDetail']

export function ListingDetailPage() {
  const { ref } = useParams<{ ref: string }>()
  const [subscribeOpen, setSubscribeOpen] = useState(false)
  const [reportOpen, setReportOpen] = useState(false)
  const [reportReason, setReportReason] = useState('')
  const [reportError, setReportError] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const submitReport = useMutation({
    mutationFn: async () => {
      assertOk(
        await apiClient.POST('/marketplace/listings/{ref}/report', {
          params: { path: { ref: ref! } },
          body: { reason: reportReason },
        }),
      )
    },
    onSuccess: () => {
      toast('举报已提交，运营团队会尽快处理')
      setReportOpen(false)
      setReportReason('')
    },
    onError: (err) => setReportError(err instanceof ApiError ? err.message : '提交失败'),
  })

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
              <Ref tone="blueprint">{listing.resource_type}</Ref>
              <span className="text-caption inline-flex items-center gap-1 text-ink-700">
                <Lock className="size-3" aria-hidden />
                黑盒
              </span>
            </div>
            <h1 className="text-display-lg mt-space-2 text-ink-900">{listing.display_meta.display_name}</h1>
            <p className="text-body-sm mt-space-2 text-ink-500">
              @{listing.author.display_name} · v{listing.version} · {listing.subscriber_count} 人订阅 · 运行{' '}
              {listing.run_count} 次
            </p>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-display-md">简介</CardTitle>
            </CardHeader>
            <CardContent className="text-body-md text-ink-700">{listing.display_meta.description}</CardContent>
          </Card>

          {listing.display_meta.usage && (
            <Card>
              <CardHeader>
                <CardTitle className="text-display-md">怎么用</CardTitle>
              </CardHeader>
              <CardContent className="text-body-md whitespace-pre-wrap text-ink-700">
                {listing.display_meta.usage}
              </CardContent>
            </Card>
          )}

          {listing.display_meta.io_description && (
            <Card>
              <CardHeader>
                <CardTitle className="text-display-md">输入输出</CardTitle>
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

          <button
            type="button"
            onClick={() => setReportOpen(true)}
            className="text-caption inline-flex w-fit items-center gap-1 text-ink-500 hover:text-ink-700"
          >
            <Flag className="size-3" aria-hidden />
            举报该资源
          </button>
        </div>

        <div className="flex flex-col gap-space-5 self-start lg:sticky lg:top-space-6">
          <Card>
            <CardContent className="pt-space-6">
              {listing.subscribed ? (
                <span className="text-label-md flex w-full items-center justify-center gap-space-2 rounded-sm border border-moss bg-moss-tint py-space-2 text-moss">
                  <span aria-hidden className="size-1.5 rounded-full bg-moss" />
                  已订阅
                </span>
              ) : (
                <button
                  type="button"
                  onClick={() => setSubscribeOpen(true)}
                  className="h-11 w-full rounded-sm bg-blueprint text-body-md font-medium text-white hover:bg-blueprint/90"
                >
                  订阅 v{listing.version}
                </button>
              )}
            </CardContent>
          </Card>

          {constraints && (
            <Card>
              <CardHeader>
                <CardTitle className="text-display-md flex items-center gap-space-2">
                  <TriangleAlert className="size-4 text-signal" aria-hidden />
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
                <CardTitle className="text-display-md">版本历史</CardTitle>
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

      <Dialog
        open={reportOpen}
        onOpenChange={(open) => {
          setReportOpen(open)
          if (!open) setReportError(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>举报该资源</DialogTitle>
          </DialogHeader>
          <Textarea
            value={reportReason}
            onChange={(e) => setReportReason(e.target.value)}
            placeholder="描述遇到的问题，例如内容不当、功能与描述严重不符……"
            className="min-h-[100px]"
          />
          {reportError && (
            <p role="alert" className="text-body-sm text-rust">
              {reportError}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setReportOpen(false)} disabled={submitReport.isPending}>
              取消
            </Button>
            <Button
              disabled={!reportReason.trim() || submitReport.isPending}
              onClick={() => {
                setReportError(null)
                submitReport.mutate()
              }}
            >
              提交举报
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
