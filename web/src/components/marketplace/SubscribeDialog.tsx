import { useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']

export function SubscribeDialog({
  listing,
  open,
  onOpenChange,
  onSubscribed,
}: {
  listing: ListingSummary | null
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubscribed: () => void
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!listing) return
    setPending(true)
    setError(null)
    try {
      unwrap(await apiClient.POST('/marketplace/listings/{id}/subscribe', { params: { path: { id: listing.id } } }))
      onSubscribed()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '订阅失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>订阅 {listing?.display_meta.display_name}</DialogTitle>
          <DialogDescription>
            将绑定到 v{listing?.version}（快照隔离）——作者后续发布新版本不会自动生效，需要你在「我的订阅」里显式升级。
          </DialogDescription>
        </DialogHeader>
        {error && (
          <p role="alert" className="text-body-sm text-rust">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending} onClick={confirm}>
            {pending ? '订阅中…' : `订阅 v${listing?.version}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
