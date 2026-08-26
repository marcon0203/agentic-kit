import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Plugin = components['schemas']['Plugin']

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function pluginPermissions(p: Plugin): string[] {
  const manifest = (p.manifest ?? {}) as { requires?: { permissions?: string[] } }
  return manifest.requires?.permissions ?? []
}

/**
 * 插件审核队列——每一行是一个申请上架（visibility=public）但还没审核
 * （review_status=pending）的版本。通过后进入 GET /plugins/market；驳回
 * 后发布者需要重新切换可见性才能再次申请。
 */
export function PluginModerationTab() {
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)
  const [reviewedIds, setReviewedIds] = useState<Set<string>>(new Set())

  const query = useQuery({
    queryKey: ['ops-moderation-plugins'],
    queryFn: async () => unwrap<{ items: Plugin[] }>(await apiClient.GET('/moderation/plugins', {})),
  })

  const review = useMutation({
    mutationFn: async ({ id, approve }: { id: string; approve: boolean }) => {
      assertOk(
        await apiClient.POST('/moderation/plugins/{id}/review', {
          params: { path: { id } },
          body: { approve },
        }),
      )
      return { id, approve }
    },
    onSuccess: ({ id, approve }) => {
      setReviewedIds((prev) => new Set(prev).add(id))
      toast(approve ? '已通过，插件已进入市场' : '已驳回')
      queryClient.invalidateQueries({ queryKey: ['ops-moderation-plugins'] })
      queryClient.invalidateQueries({ queryKey: ['plugins', 'market'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : '操作失败')
    },
  })

  const items = (query.data?.items ?? []).filter((p) => !reviewedIds.has(p.id))

  return (
    <div className="flex flex-col gap-space-6">
      {query.isLoading && <ListSkeleton rows={6} />}
      {query.isError && <ErrorPanel message="插件审核队列加载失败" onRetry={() => query.refetch()} />}
      {actionError && (
        <p role="alert" className="text-body-sm text-rust">
          {actionError}
        </p>
      )}

      {query.isSuccess && items.length === 0 && (
        <EmptyState title="审核队列是空的" description="有发布者申请把插件上架到市场时会出现在这里。" />
      )}

      {items.length > 0 && (
        <div className="flex flex-col gap-space-3">
          {items.map((p) => {
            const permissions = pluginPermissions(p)
            return (
              <div key={p.id} className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface px-space-4 py-space-3">
                <div className="flex flex-wrap items-center justify-between gap-space-2">
                  <div className="flex flex-col">
                    <span className="text-body-md text-ink-900">{p.display_name || p.plugin_id}</span>
                    <span className="text-ref font-mono text-ink-500">
                      {p.plugin_id}@{p.version}
                    </span>
                  </div>
                  <span className="text-ref text-ink-500">{fmtTime(p.created_at)}</span>
                </div>

                {permissions.length > 0 && (
                  <div className="flex flex-wrap gap-space-1">
                    {permissions.map((perm) => (
                      <span key={perm} className="text-caption rounded-sm bg-surface-muted px-space-2 py-0.5 font-mono text-ink-700">
                        {perm}
                      </span>
                    ))}
                  </div>
                )}

                <div className="flex justify-end gap-space-2">
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={review.isPending}
                    onClick={() => {
                      setActionError(null)
                      review.mutate({ id: p.id, approve: false })
                    }}
                  >
                    驳回
                  </Button>
                  <Button
                    size="sm"
                    disabled={review.isPending}
                    onClick={() => {
                      setActionError(null)
                      review.mutate({ id: p.id, approve: true })
                    }}
                  >
                    通过
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
