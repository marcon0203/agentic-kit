import { Loader2 } from 'lucide-react'

/**
 * 路由级代码分割（React.lazy）的 Suspense 兜底。App.tsx 把整棵 <Routes>
 * 包在一个 Suspense 里，页面切换时短暂看到这个，而不是白屏。
 */
export function RouteFallback() {
  return (
    <div className="flex flex-1 items-center justify-center py-space-8">
      <Loader2 className="size-8 animate-spin text-ink-500" aria-label="加载中" />
    </div>
  )
}
