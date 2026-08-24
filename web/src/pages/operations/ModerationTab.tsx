import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Report = components['schemas']['Report']

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

export function ModerationTab() {
  const queryClient = useQueryClient()
  const [takedownTarget, setTakedownTarget] = useState<Report | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resolvedIds, setResolvedIds] = useState<Set<string>>(new Set())

  const query = useQuery({
    queryKey: ['ops-moderation-reports'],
    queryFn: async () => unwrap<{ items: Report[] }>(await apiClient.GET('/moderation/reports', { params: { query: { limit: 50 } } })),
  })

  const resolve = useMutation({
    mutationFn: async ({ id, action }: { id: string; action: 'dismiss' | 'takedown' }) => {
      assertOk(
        await apiClient.POST('/moderation/reports/{id}/resolve', {
          params: { path: { id } },
          body: { action },
        }),
      )
      return { id, action }
    },
    onSuccess: ({ id, action }) => {
      setResolvedIds((prev) => new Set(prev).add(id))
      toast(action === 'takedown' ? '已下架' : '已忽略该举报')
      setTakedownTarget(null)
      queryClient.invalidateQueries({ queryKey: ['ops-moderation-reports'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : '操作失败')
    },
  })

  const items = (query.data?.items ?? []).filter((r) => !resolvedIds.has(r.id))

  return (
    <div className="flex flex-col gap-space-6">
      {query.isLoading && <ListSkeleton rows={6} />}
      {query.isError && <ErrorPanel message="举报队列加载失败" onRetry={() => query.refetch()} />}
      {actionError && (
        <p role="alert" className="text-body-sm text-rust">
          {actionError}
        </p>
      )}

      {query.isSuccess && items.length === 0 && <EmptyState
          title="举报队列是空的"
          description="有人举报广场上的资源时会出现在这里，等你决定驳回还是下架。"
        />}

      {items.length > 0 && (
        <>
          <div className="hidden overflow-x-auto rounded-lg border border-border min-[901px]:block">
            <table className="w-full min-w-[720px] border-collapse">
              <thead>
                <tr className="border-b border-border text-left">
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">被举报资源</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">举报理由</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">提交时间</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700"></th>
                </tr>
              </thead>
              <tbody>
                {items.map((r) => (
                  <tr key={r.id} className="border-b border-border last:border-0 hover:bg-surface-muted">
                    <td className="text-body-md px-space-4 py-space-3 font-mono text-ink-900">{r.listing_ref}</td>
                    <td className="text-body-sm px-space-4 py-space-3 text-ink-700">{r.reason}</td>
                    <td className="text-ref tabular px-space-4 py-space-3 text-ink-700">{fmtTime(r.created_at)}</td>
                    <td className="px-space-4 py-space-3 text-right">
                      <div className="flex justify-end gap-space-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={resolve.isPending}
                          onClick={() => {
                            setActionError(null)
                            resolve.mutate({ id: r.id, action: 'dismiss' })
                          }}
                        >
                          忽略
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          disabled={resolve.isPending}
                          onClick={() => {
                            setActionError(null)
                            setTakedownTarget(r)
                          }}
                        >
                          下架
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="flex flex-col gap-space-3 min-[901px]:hidden">
            {items.map((r) => (
              <div key={r.id} className="flex flex-col gap-space-2 rounded-lg border border-border bg-surface px-space-4 py-space-3">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-body-md text-ink-900">{r.listing_ref}</span>
                  <span className="text-ref text-ink-500">{fmtTime(r.created_at)}</span>
                </div>
                <p className="text-body-sm text-ink-700">{r.reason}</p>
                <div className="flex gap-space-2">
                  <Button
                    variant="outline"
                    size="sm"
                    className="flex-1"
                    disabled={resolve.isPending}
                    onClick={() => {
                      setActionError(null)
                      resolve.mutate({ id: r.id, action: 'dismiss' })
                    }}
                  >
                    忽略
                  </Button>
                  <Button
                    variant="destructive"
                    size="sm"
                    className="flex-1"
                    disabled={resolve.isPending}
                    onClick={() => {
                      setActionError(null)
                      setTakedownTarget(r)
                    }}
                  >
                    下架
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <Dialog open={!!takedownTarget} onOpenChange={(open) => !open && setTakedownTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认下架该资源？</DialogTitle>
          </DialogHeader>
          <p className="text-body-sm text-ink-700">
            资源 <span className="font-mono text-ink-900">{takedownTarget?.listing_ref}</span> 有{' '}
            <strong className="text-ink-900">{takedownTarget?.subscriber_count ?? 0}</strong> 个存量订阅者，下架后他们将无法继续使用。此操作不可撤销。
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setTakedownTarget(null)} disabled={resolve.isPending}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={resolve.isPending}
              onClick={() => takedownTarget && resolve.mutate({ id: takedownTarget.id, action: 'takedown' })}
            >
              确认下架
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
