import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { StartRunCard } from '@/components/run/StartRunCard'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import { useHasModelProvider } from '@/lib/models/useHasModelProvider'
import type { components } from '@/lib/api/schema'

type Bundle = components['schemas']['Bundle']

export function BundleListPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const { hasProvider, isLoading: providerLoading } = useHasModelProvider()
  const runBlocked = !providerLoading && !hasProvider

  const query = useQuery({
    queryKey: ['bundles'],
    queryFn: async () => unwrap<{ items: Bundle[] }>(await apiClient.GET('/bundles', {})),
  })

  async function remove(ref: string) {
    setDeleteError(null)
    try {
      assertOk(await apiClient.DELETE('/bundles/{ref}', { params: { path: { ref } } }))
      queryClient.invalidateQueries({ queryKey: ['bundles'] })
    } catch (err) {
      setDeleteError(err instanceof ApiError ? err.message : '删除失败')
    }
  }

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-8">
      <div className="flex flex-col gap-space-6">
        <h1 className="text-headline-md text-ink-900">应用中心</h1>

        {query.isLoading && <ListSkeleton />}
        {query.isError && <ErrorPanel message="Bundle 列表加载失败" onRetry={() => query.refetch()} />}
        {deleteError && (
          <p role="alert" className="text-body-sm text-[var(--color-error)]">
            {deleteError}
          </p>
        )}

        {query.isSuccess && items.length === 0 && (
          <EmptyState
            title="还没有创建任何 Bundle"
            description="Bundle 编排多个 Agent 协作完成一次任务，是运行的最小单位。"
          />
        )}

        {items.length > 0 && (
          <ul className="flex flex-col gap-space-2">
            {items.map((b) => (
              <li key={b.id} className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4">
                <div className="flex-1">
                  <div className="flex items-center gap-space-2">
                    <span className="font-mono text-body-md text-ink-900">{b.bundle_ref}</span>
                    <span className="text-caption text-ink-500">v{b.version}</span>
                  </div>
                  {b.definition.description && <p className="text-body-sm text-ink-700">{b.definition.description}</p>}
                </div>
                <Badge variant={b.status === 1 ? 'default' : 'secondary'}>{b.status === 1 ? '已启用' : '已停用'}</Badge>
                <Button
                  size="sm"
                  disabled={runBlocked}
                  title={runBlocked ? '请先在模型中心接入模型 Provider' : undefined}
                  onClick={() => navigate('/apps', { state: { quickStartBundleRef: b.bundle_ref } })}
                >
                  运行
                </Button>
                <Button variant="secondary" size="sm" onClick={() => remove(b.bundle_ref)}>
                  删除
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <StartRunCard />
    </div>
  )
}
