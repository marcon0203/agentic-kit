import { useState } from 'react'
import { Wifi, WifiOff, RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { StatusChip, type PlatformStatus } from '@/components/run/StatusChip'
import { StopRunDialog } from '@/components/run/StopRunDialog'
import type { StreamStatus } from '@/lib/runs/useRunEvents'

const STREAM_LABEL: Record<StreamStatus, string> = {
  connecting: '事件流连接中',
  open: '事件流已连接',
  reconnecting: '重新连接中',
  closed: '已结束',
  error: '连接中断',
}

export function RunHeader({
  runId,
  status,
  streamStatus,
  totalTokens,
  costUsd,
  onReconnect,
  onStop,
}: {
  runId: string
  status: PlatformStatus
  streamStatus: StreamStatus
  totalTokens: number
  costUsd: number
  onReconnect: () => void
  onStop: () => Promise<void>
}) {
  const [stopOpen, setStopOpen] = useState(false)
  const isRunning = status === 'running'

  return (
    <div className="mb-space-6 flex items-center justify-between rounded-lg border border-border bg-surface px-space-6 py-space-4">
      <div className="flex items-center gap-space-4">
        <code className="text-ref-lg text-ink-900">{runId}</code>
        <StatusChip status={status} />
        <span className="text-caption inline-flex items-center gap-1 text-ink-500">
          {streamStatus === 'open' || streamStatus === 'connecting' ? (
            <Wifi className="size-3.5" aria-hidden />
          ) : (
            <WifiOff className="size-3.5" aria-hidden />
          )}
          {STREAM_LABEL[streamStatus]}
        </span>
      </div>

      <div className="flex items-center gap-space-3">
        {streamStatus === 'error' && (
          <Button variant="outline" size="sm" onClick={onReconnect}>
            <RefreshCw className="size-3.5" aria-hidden />
            重新连接
          </Button>
        )}
        {isRunning && (
          <Button variant="outline" size="sm" onClick={() => setStopOpen(true)}>
            停止运行
          </Button>
        )}
      </div>

      <StopRunDialog
        open={stopOpen}
        onOpenChange={setStopOpen}
        totalTokens={totalTokens}
        costUsd={costUsd}
        onConfirm={onStop}
      />
    </div>
  )
}
