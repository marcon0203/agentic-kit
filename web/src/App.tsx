import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate, useSearchParams } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { HomePage } from '@/pages/HomePage'
import { NewRunPage } from '@/pages/NewRunPage'
import { RunPage } from '@/pages/RunPage'
import { AppsLayout } from '@/pages/AppsLayout'
import { SettingsLayout } from '@/pages/SettingsLayout'
import { ModelCatalogAdminPage } from '@/pages/ModelCatalogAdminPage'
import { UsersPage } from '@/pages/UsersPage'
import { RolesPage } from '@/pages/RolesPage'
import { MarketplaceBrowsePage } from '@/pages/MarketplaceBrowsePage'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'
import { ResourceKindPage } from '@/pages/ResourceCenterPage'
import { MyListingsPage } from '@/pages/MyListingsPage'
import { MySubscriptionsPage } from '@/pages/MySubscriptionsPage'
import { ModelProviderPage } from '@/pages/ModelProviderPage'
import { ListingDetailPage } from '@/pages/ListingDetailPage'
import { BundleEditorPage } from '@/pages/BundleEditorPage'
import { McpServerEditorPage } from '@/pages/McpServerEditorPage'
import { SkillUploadPage } from '@/pages/SkillUploadPage'
import { SkillSourcesPage } from '@/pages/SkillSourcesPage'
import { SkillSourceDetailPage } from '@/pages/SkillSourceDetailPage'
import { SkillMarketDetailPage } from '@/pages/SkillMarketDetailPage'
import { ComponentWizardPage } from '@/pages/ComponentWizardPage'
import { ComponentPlazaPage } from '@/pages/ComponentPlazaPage'
import { AgentStudioPage } from '@/pages/AgentStudioPage'
import { OpsLayout, RequireAdmin } from '@/pages/OpsLayout'
import { RunMonitorTab } from '@/pages/operations/RunMonitorTab'
import { CostAnalysisTab } from '@/pages/operations/CostAnalysisTab'
import { AuditLogTab } from '@/pages/operations/AuditLogTab'
import { ModerationTab } from '@/pages/operations/ModerationTab'
import { PluginModerationTab } from '@/pages/operations/PluginModerationTab'

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
          {/* 智能体工作台是整屏页面（左配置 / 右试运行），刻意放在 AppShell
              外面：它自己带顶栏和左侧导航，再套一层应用外壳只会把右边的
              测试区挤没。 */}
          <Route
            path="/agents/new"
            element={
              <ProtectedRoute>
                <AgentStudioPage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/agents/:id/edit"
            element={
              <ProtectedRoute>
                <AgentStudioPage />
              </ProtectedRoute>
            }
          />
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
              <Route path="bundles/new" element={<BundleEditorPage />} />
              <Route path="bundles/:ref/edit" element={<BundleEditorPage />} />
              <Route path="agents" element={<AgentDefinitionPage />} />
              <Route path="tool" element={<ComponentPlazaPage />} />
              <Route path="tool/new" element={<ComponentWizardPage />} />
              <Route path="skill" element={<ResourceKindPage type="skill" />} />
              <Route path="skill/new" element={<SkillUploadPage />} />
              <Route path="skill/market/:sourceId/:slug" element={<SkillMarketDetailPage />} />
              <Route path="mcp" element={<ResourceKindPage type="mcp" />} />
              <Route path="mcp/new" element={<McpServerEditorPage />} />
              <Route path="knowledge_base" element={<ResourceKindPage type="knowledge_base" />} />
              <Route path="memory" element={<ResourceKindPage type="memory" />} />
              <Route path="publish" element={<MyListingsPage />} />
              <Route path="subscriptions" element={<MySubscriptionsPage />} />
            </Route>
            <Route
              path="/runs/new"
              element={
                <ProtectedRoute>
                  <NewRunPage />
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
                  <OpsLayout />
                </ProtectedRoute>
              }
            >
              <Route index element={<Navigate to="monitor" replace />} />
              <Route path="monitor" element={<RunMonitorTab />} />
              <Route path="cost" element={<CostAnalysisTab />} />
              <Route path="audit" element={<AuditLogTab />} />
              <Route
                path="moderation"
                element={
                  <RequireAdmin>
                    <ModerationTab />
                  </RequireAdmin>
                }
              />
              <Route
                path="plugin-moderation"
                element={
                  <RequireAdmin>
                    <PluginModerationTab />
                  </RequireAdmin>
                }
              />
            </Route>
            <Route
              path="/settings"
              element={
                <ProtectedRoute>
                  <SettingsLayout />
                </ProtectedRoute>
              }
            >
              <Route index element={<Navigate to="providers" replace />} />
              <Route path="providers" element={<ModelCatalogAdminPage />} />
              <Route path="skill-sources" element={<SkillSourcesPage />} />
              <Route path="skill-sources/:id" element={<SkillSourceDetailPage />} />
              <Route path="users" element={<UsersPage />} />
              <Route path="roles" element={<RolesPage />} />
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
      <AuthModal />
      <Toaster />
    </QueryClientProvider>
  )
}
