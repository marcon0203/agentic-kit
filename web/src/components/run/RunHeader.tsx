import { useState } from 'react'
import { RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { PlatformStatus } from '@/components/run/StatusChip'
import { StopRunDialog } from '@/components/run/StopRunDialog'
import type { StreamStatus } from '@/lib/runs/useRunEvents'

/**
 * 只在真的需要用户做点什么的时候才露面：事件流断了给一个重连按钮，正在
 * 跑给一个停止按钮。"已完成"/"已结束"这类状态本身不需要额外提示——
 * 聊天气泡和输入框是不是能接着发消息，用户自己看得出来，专门画一行反
 * 而是噪音。
 */
export function RunHeader({
  status,
  streamStatus,
  totalTokens,
  costUsd,
  onReconnect,
  onStop,
}: {
  status: PlatformStatus
  streamStatus: StreamStatus
  totalTokens: number
  costUsd: number
  onReconnect: () => void
  onStop: () => Promise<void>
}) {
  const [stopOpen, setStopOpen] = useState(false)
  const isRunning = status === 'running'

  if (!isRunning && streamStatus !== 'error') return null

  return (
    <div className="mb-space-4 flex items-center justify-end gap-space-3">
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
