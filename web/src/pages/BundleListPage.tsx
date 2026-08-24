import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { EmptyRail } from '@/components/common/Rail'
import { Ref, Section } from '@/components/common/Page'
import { cn } from '@/lib/utils'
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
      setDeleteError(err instanceof ApiError ? err.message : '删除没能完成，请再试一次')
    }
  }

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-8">
      <Section
        title="我的 Bundle"
        aside={
          <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => navigate('/bundles/new')}>
            新建 Bundle
          </Button>
        }
      >
        {query.isLoading && <ListSkeleton />}
        {query.isError && (
          <ErrorPanel message="Bundle 列表没能加载出来" onRetry={() => query.refetch()} />
        )}
        {deleteError && (
          <p role="alert" className="text-body-sm text-rust">
            {deleteError}
          </p>
        )}

        {query.isSuccess && items.length === 0 && (
          <EmptyRail
            title="编排你的第一次协作"
            description="Bundle 决定谁先做、谁并行、哪一步要停下来等人。它是运行的最小单位——有了 Bundle 才能发起运行。"
            action={
              <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => navigate('/bundles/new')}>
                新建 Bundle
              </Button>
            }
          />
        )}

        {items.length > 0 && (
          <ul className="overflow-hidden rounded-lg border border-border bg-surface">
            {items.map((b) => (
              <li
                key={b.id}
                className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0"
              >
                <span
                  aria-hidden
                  className={cn(
                    'size-2 shrink-0 rounded-full',
                    b.status === 1 ? 'bg-moss' : 'bg-border-strong',
                  )}
                />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex items-center gap-space-2">
                    <Ref>{b.bundle_ref}</Ref>
                    <span className="text-caption tabular text-ink-500">v{b.version}</span>
                  </span>
                  {b.definition.description && (
                    <span className="text-body-sm truncate text-ink-700">
                      {b.definition.description}
                    </span>
                  )}
                </span>
                <Button
                  size="sm"
                  disabled={runBlocked}
                  title={runBlocked ? '先去模型广场接入一个 Provider，才能发起运行' : undefined}
                  onClick={() => navigate('/apps', { state: { quickStartBundleRef: b.bundle_ref } })}
                >
                  运行
                </Button>
                <Button variant="ghost" size="sm" onClick={() => remove(b.bundle_ref)}>
                  删除
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <StartRunCard />
    </div>
  )
}
