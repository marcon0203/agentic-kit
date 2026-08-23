import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { HomePage } from '@/pages/HomePage'
import { PagePlaceholder } from '@/pages/PagePlaceholder'
import { RunPage } from '@/pages/RunPage'
import { StartRunCard } from '@/components/run/StartRunCard'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/" element={<HomePage />} />
            <Route
              path="/marketplace"
              element={<PagePlaceholder title="应用广场" note="订阅、发布与浏览由 spec-16 实现。" />}
            />
            <Route
              path="/apps"
              element={
                <ProtectedRoute>
                  <div className="flex flex-col gap-space-6">
                    <h1 className="text-headline-md text-ink-900">应用中心</h1>
                    <p className="text-body-md max-w-[640px] text-ink-700">
                      完整的 Bundle 列表与可视化编排由 spec-15 / spec-17 实现；这里先提供一个最小的运行入口，
                      衔接 spec-14 的 Chat 运行详情页。
                    </p>
                    <StartRunCard />
                  </div>
                </ProtectedRoute>
              }
            />
            <Route
              path="/runs/:runId"
              element={
                <ProtectedRoute>
                  <RunPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/resources"
              element={
                <ProtectedRoute>
                  <PagePlaceholder title="资源中心" note="Agent / Tool / MCP / 知识库管理由 spec-15 实现。" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/models"
              element={
                <ProtectedRoute>
                  <PagePlaceholder title="模型中心" note="模型 Provider 接入与凭证管理由 spec-15 实现。" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/ops"
              element={
                <ProtectedRoute>
                  <PagePlaceholder title="运营中心" note="用量看板与运行历史由 spec-18 实现。" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/settings"
              element={
                <ProtectedRoute>
                  <PagePlaceholder title="系统设置" note="账号与平台设置由后续任务实现。" />
                </ProtectedRoute>
              }
            />
          </Route>
        </Routes>
      </BrowserRouter>
      <AuthModal />
      <Toaster />
    </QueryClientProvider>
  )
}
