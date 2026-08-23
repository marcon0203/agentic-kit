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

type Subscription = components['schemas']['Subscription']

export function UpgradeDialog({
  subscription,
  open,
  onOpenChange,
  onUpgraded,
}: {
  subscription: Subscription | null
  open: boolean
  onOpenChange: (v: boolean) => void
  onUpgraded: () => void
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!subscription?.latest_version) return
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.POST('/marketplace/subscriptions/{id}/upgrade', {
          params: { path: { id: subscription.id } },
          body: { target_version: subscription.latest_version },
        }),
      )
      onUpgraded()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '升级失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>升级到 v{subscription?.latest_version}</DialogTitle>
          <DialogDescription>
            当前订阅版本 v{subscription?.subscribed_version}。升级后旧版本的运行历史仍可查，此操作不会自动发生。
          </DialogDescription>
        </DialogHeader>
        {subscription?.latest_changelog && (
          <div className="rounded-md bg-surface-muted p-space-4">
            <p className="text-label-md mb-space-2 text-ink-900">更新说明</p>
            <p className="text-body-sm whitespace-pre-wrap text-ink-700">{subscription.latest_changelog}</p>
          </div>
        )}
        {error && (
          <p role="alert" className="text-body-sm text-[var(--color-error)]">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending} onClick={confirm}>
            {pending ? '升级中…' : `升级到 v${subscription?.latest_version}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
