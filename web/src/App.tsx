import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate, useSearchParams } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { HomePage } from '@/pages/HomePage'
import { PagePlaceholder } from '@/pages/PagePlaceholder'
import { RunPage } from '@/pages/RunPage'
import { AppsLayout } from '@/pages/AppsLayout'
import { MarketplaceBrowsePage } from '@/pages/MarketplaceBrowsePage'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'
import { ResourceKindPage } from '@/pages/ResourceCenterPage'
import { MyListingsPage } from '@/pages/MyListingsPage'
import { MySubscriptionsPage } from '@/pages/MySubscriptionsPage'
import { ModelProviderPage } from '@/pages/ModelProviderPage'
import { ListingDetailPage } from '@/pages/ListingDetailPage'
import { BundleEditorPage } from '@/pages/BundleEditorPage'
import { OperationsPage } from '@/pages/OperationsPage'

/**
 * 应用广场（发布市场）已经并入应用中心（/apps/<section>），旧的
 * /marketplace?tab=x 链接保留跳转而不是直接失效——tab 的取值就是新路由的
 * 段名，原样拼进路径即可。
 */
function MarketplaceRedirect() {
  const [searchParams] = useSearchParams()
  const tab = searchParams.get('tab')
  return <Navigate to={tab ? `/apps/${tab}` : '/apps/browse'} replace />
}

// 资源中心同样并入 /apps（二级菜单下每个资源类型各自一条路由），旧链接原样跳转到 Tool。
function ResourcesRedirect() {
  return <Navigate to="/apps/tool" replace />
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
                  <AppsLayout />
                </ProtectedRoute>
              }
            >
              <Route index element={<Navigate to="browse" replace />} />
              <Route path="browse" element={<MarketplaceBrowsePage />} />
              <Route path="bundles" element={<BundleListPage />} />
              <Route path="agents" element={<AgentDefinitionPage />} />
              <Route path="tool" element={<ResourceKindPage type="tool" />} />
              <Route path="skill" element={<ResourceKindPage type="skill" />} />
              <Route path="mcp" element={<ResourceKindPage type="mcp" />} />
              <Route path="knowledge_base" element={<ResourceKindPage type="knowledge_base" />} />
              <Route path="memory" element={<ResourceKindPage type="memory" />} />
              <Route path="publish" element={<MyListingsPage />} />
              <Route path="subscriptions" element={<MySubscriptionsPage />} />
            </Route>
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
