import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { HomePage } from '@/pages/HomePage'
import { PagePlaceholder } from '@/pages/PagePlaceholder'
import { RunPage } from '@/pages/RunPage'
import { AppsPage } from '@/pages/AppsPage'
import { ResourceCenterPage } from '@/pages/ResourceCenterPage'
import { ModelProviderPage } from '@/pages/ModelProviderPage'

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
                  <AppsPage />
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
                  <ResourceCenterPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/models"
              element={
                <ProtectedRoute>
                  <ModelProviderPage />
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
