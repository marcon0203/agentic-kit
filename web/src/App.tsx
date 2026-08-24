import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate, useSearchParams } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { HomePage } from '@/pages/HomePage'
import { PagePlaceholder } from '@/pages/PagePlaceholder'
import { RunPage } from '@/pages/RunPage'
import { AppsPage } from '@/pages/AppsPage'
import { ModelProviderPage } from '@/pages/ModelProviderPage'
import { ListingDetailPage } from '@/pages/ListingDetailPage'
import { BundleEditorPage } from '@/pages/BundleEditorPage'
import { OperationsPage } from '@/pages/OperationsPage'

/**
 * 应用广场（发布市场）已经并入应用中心（/apps），旧的 /marketplace 链接
 * 保留跳转而不是直接失效——tab 参数的取值在两边是一样的字符串，原样带过去
 * 就对得上新页面的二级菜单。
 */
function MarketplaceRedirect() {
  const [searchParams] = useSearchParams()
  const tab = searchParams.get('tab')
  return <Navigate to={tab ? `/apps?tab=${tab}` : '/apps'} replace />
}

// 资源中心同样并入 /apps（二级菜单的独立 tab），旧链接原样跳转到 Tool。
function ResourcesRedirect() {
  return <Navigate to="/apps?tab=tool" replace />
}

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
            <Route path="/marketplace" element={<MarketplaceRedirect />} />
            <Route
              path="/marketplace/listing/:ref"
              element={
                <ProtectedRoute>
                  <ListingDetailPage />
                </ProtectedRoute>
              }
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
              path="/bundles/new"
              element={
                <ProtectedRoute>
                  <BundleEditorPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/bundles/:ref/edit"
              element={
                <ProtectedRoute>
                  <BundleEditorPage />
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
            <Route path="/resources" element={<ResourcesRedirect />} />
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
                  <OperationsPage />
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
