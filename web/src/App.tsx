import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

function HomePlaceholder() {
  return (
    <div className="mx-auto flex min-h-screen max-w-3xl items-center justify-center p-8">
      <Card className="w-full">
        <CardHeader>
          <CardTitle>AI Agent 平台</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground text-sm">
          脚手架已就绪。路由、鉴权弹窗与真实页面由 spec-13-fe-foundation 及后续任务实现。
        </CardContent>
      </Card>
    </div>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<HomePlaceholder />} />
        </Routes>
      </BrowserRouter>
      <Toaster />
    </QueryClientProvider>
  )
}
