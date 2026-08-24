import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

export function StopRunDialog({
  open,
  onOpenChange,
  totalTokens,
  costUsd,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  totalTokens: number
  costUsd: number
  onConfirm: () => Promise<void>
}) {
  const [pending, setPending] = useState(false)

  async function confirm() {
    setPending(true)
    try {
      await onConfirm()
      onOpenChange(false)
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>确定停止这次运行？</DialogTitle>
          <DialogDescription>
            已生成的产出会保留；<strong className="text-ink-900">已消耗的 Token 无法退回</strong>
            （当前已消耗 {totalTokens.toLocaleString()} tokens ≈ ${costUsd.toFixed(4)}）。
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            继续运行
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={pending}>
            {pending ? '停止中…' : '停止'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
