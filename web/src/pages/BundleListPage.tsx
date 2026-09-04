import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { EmptyRail } from '@/components/common/Rail'
import { Section } from '@/components/common/Page'
import { BundleCard } from '@/components/bundle/BundleCard'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import { useHasModelProvider } from '@/lib/models/useHasModelProvider'
import type { components } from '@/lib/api/schema'

type Bundle = components['schemas']['Bundle']

export function BundleListPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [deleteError, setDeleteError] = useState<string | null>(null)
  // Deleting is not undoable and a card's actions sit close together, so it
  // asks first — the old flat list deleted on a single click.
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const { hasProvider, isLoading: providerLoading } = useHasModelProvider()
  const runBlocked = !providerLoading && !hasProvider

  const query = useQuery({
    queryKey: ['bundles'],
    queryFn: async () => unwrap<{ items: Bundle[] }>(await apiClient.GET('/bundles', {})),
  })

  async function confirmDelete() {
    if (!pendingDelete) return
    setDeleting(true)
    setDeleteError(null)
    try {
      assertOk(await apiClient.DELETE('/bundles/{ref}', { params: { path: { ref: pendingDelete } } }))
      queryClient.invalidateQueries({ queryKey: ['bundles'] })
      setPendingDelete(null)
    } catch (err) {
      setDeleteError(err instanceof ApiError ? err.message : '删除没能完成，请再试一次')
    } finally {
      setDeleting(false)
    }
  }

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-8">
      <Section
        title="我的应用"
        aside={
          <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => navigate('/apps/bundles/new')}>
            新建应用
          </Button>
        }
      >
        {query.isLoading && <ListSkeleton />}
        {query.isError && <ErrorPanel message="应用列表没能加载出来" onRetry={() => query.refetch()} />}
        {deleteError && (
          <p role="alert" className="text-body-sm text-rust">
            {deleteError}
          </p>
        )}

        {query.isSuccess && items.length === 0 && (
          <EmptyRail
            title="编排你的第一个应用"
            description="一个应用（Bundle）决定谁先做、谁并行、哪一步要停下来等人。有了应用才能发起运行。"
            action={
              <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => navigate('/apps/bundles/new')}>
                新建应用
              </Button>
            }
          />
        )}

        {items.length > 0 && (
          <div className="grid grid-cols-1 gap-space-4 md:grid-cols-2 xl:grid-cols-3">
            {items.map((b) => (
              <BundleCard
                key={b.id}
                bundle={b}
                runBlocked={runBlocked}
                onRun={(ref) => navigate(`/runs/new?bundle=${encodeURIComponent(ref)}`)}
                onEdit={(ref) => navigate(`/apps/bundles/${encodeURIComponent(ref)}/edit`)}
                onDelete={setPendingDelete}
                onPublish={(bundle) =>
                  navigate(
                    `/apps/publish?type=bundle&ref=${encodeURIComponent(bundle.bundle_ref)}&version=${encodeURIComponent(bundle.version)}`,
                  )
                }
              />
            ))}
          </div>
        )}
      </Section>

      <Dialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除应用</DialogTitle>
            <DialogDescription>
              确定删除 <span className="text-ref text-ink-900">{pendingDelete}</span> 吗？它的所有版本都会被删除，已经跑过的运行记录会保留。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(null)}>
              取消
            </Button>
            <Button variant="destructive" disabled={deleting} onClick={confirmDelete}>
              {deleting ? '删除中…' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
