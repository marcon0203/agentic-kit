import { lazy, Suspense } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate, useSearchParams } from 'react-router-dom'

import { Toaster } from '@/components/ui/sonner'
import { AppShell } from '@/components/layout/AppShell'
import { AuthModal } from '@/components/auth/AuthModal'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { RouteFallback } from '@/components/common/RouteFallback'
import { OpsLayout, RequireAdmin } from '@/pages/OpsLayout'

// 每个页面各自一个动态 import，是这批路由拆包的全部工作量——路由本来就是
// 天然的分包边界（进哪个页面才用哪个页面的代码），不需要手动 manualChunks
// 去猜哪些模块该分到一起。像 @assistant-ui/react + react-markdown（只有
// AgentStudioPage/RunPage 用到）、@xyflow/react（只有 BundleEditorPage 用
// 到）这些重依赖，分包之后自然只在真正打开那个页面时才下载。
// OpsLayout/RequireAdmin 例外：RequireAdmin 是结构性的权限门禁组件，会在
// JSX 里反复包裹其他路由元素，懒加载它换不来什么，还平添一层 Suspense。
const HomePage = lazy(() => import('@/pages/HomePage').then((m) => ({ default: m.HomePage })))
const NewRunPage = lazy(() => import('@/pages/NewRunPage').then((m) => ({ default: m.NewRunPage })))
const RunPage = lazy(() => import('@/pages/RunPage').then((m) => ({ default: m.RunPage })))
const AppsLayout = lazy(() => import('@/pages/AppsLayout').then((m) => ({ default: m.AppsLayout })))
const SettingsLayout = lazy(() => import('@/pages/SettingsLayout').then((m) => ({ default: m.SettingsLayout })))
const ModelCatalogAdminPage = lazy(() =>
  import('@/pages/ModelCatalogAdminPage').then((m) => ({ default: m.ModelCatalogAdminPage })),
)
const UsersPage = lazy(() => import('@/pages/UsersPage').then((m) => ({ default: m.UsersPage })))
const RolesPage = lazy(() => import('@/pages/RolesPage').then((m) => ({ default: m.RolesPage })))
const ApiKeysPage = lazy(() => import('@/pages/ApiKeysPage').then((m) => ({ default: m.ApiKeysPage })))
const MarketplaceBrowsePage = lazy(() =>
  import('@/pages/MarketplaceBrowsePage').then((m) => ({ default: m.MarketplaceBrowsePage })),
)
const BundleListPage = lazy(() => import('@/pages/BundleListPage').then((m) => ({ default: m.BundleListPage })))
const AgentDefinitionPage = lazy(() =>
  import('@/pages/AgentDefinitionPage').then((m) => ({ default: m.AgentDefinitionPage })),
)
const ResourceKindPage = lazy(() => import('@/pages/ResourceCenterPage').then((m) => ({ default: m.ResourceKindPage })))
const MyListingsPage = lazy(() => import('@/pages/MyListingsPage').then((m) => ({ default: m.MyListingsPage })))
const MySubscriptionsPage = lazy(() =>
  import('@/pages/MySubscriptionsPage').then((m) => ({ default: m.MySubscriptionsPage })),
)
const ModelProviderPage = lazy(() => import('@/pages/ModelProviderPage').then((m) => ({ default: m.ModelProviderPage })))
const ListingDetailPage = lazy(() => import('@/pages/ListingDetailPage').then((m) => ({ default: m.ListingDetailPage })))
const BundleEditorPage = lazy(() => import('@/pages/BundleEditorPage').then((m) => ({ default: m.BundleEditorPage })))
const McpServerEditorPage = lazy(() =>
  import('@/pages/McpServerEditorPage').then((m) => ({ default: m.McpServerEditorPage })),
)
const SkillUploadPage = lazy(() => import('@/pages/SkillUploadPage').then((m) => ({ default: m.SkillUploadPage })))
const SkillSourcesPage = lazy(() => import('@/pages/SkillSourcesPage').then((m) => ({ default: m.SkillSourcesPage })))
const SkillSourceDetailPage = lazy(() =>
  import('@/pages/SkillSourceDetailPage').then((m) => ({ default: m.SkillSourceDetailPage })),
)
const McpSourcesPage = lazy(() => import('@/pages/McpSourcesPage').then((m) => ({ default: m.McpSourcesPage })))
const McpSourceDetailPage = lazy(() =>
  import('@/pages/McpSourceDetailPage').then((m) => ({ default: m.McpSourceDetailPage })),
)
const SkillMarketDetailPage = lazy(() =>
  import('@/pages/SkillMarketDetailPage').then((m) => ({ default: m.SkillMarketDetailPage })),
)
const ComponentWizardPage = lazy(() =>
  import('@/pages/ComponentWizardPage').then((m) => ({ default: m.ComponentWizardPage })),
)
const ComponentPlazaPage = lazy(() => import('@/pages/ComponentPlazaPage').then((m) => ({ default: m.ComponentPlazaPage })))
const ComponentDetailPage = lazy(() =>
  import('@/pages/ComponentDetailPage').then((m) => ({ default: m.ComponentDetailPage })),
)
const AgentStudioPage = lazy(() => import('@/pages/AgentStudioPage').then((m) => ({ default: m.AgentStudioPage })))
const RunMonitorTab = lazy(() => import('@/pages/operations/RunMonitorTab').then((m) => ({ default: m.RunMonitorTab })))
const CostAnalysisTab = lazy(() =>
  import('@/pages/operations/CostAnalysisTab').then((m) => ({ default: m.CostAnalysisTab })),
)
const AuditLogTab = lazy(() => import('@/pages/operations/AuditLogTab').then((m) => ({ default: m.AuditLogTab })))
const ModerationTab = lazy(() => import('@/pages/operations/ModerationTab').then((m) => ({ default: m.ModerationTab })))
const PluginModerationTab = lazy(() =>
  import('@/pages/operations/PluginModerationTab').then((m) => ({ default: m.PluginModerationTab })),
)

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
        <Suspense fallback={<RouteFallback />}>
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
                {/* /new 必须排在 /:id 前面，否则 "new" 会被当成资源 id。 */}
                <Route path="tool/:id" element={<ComponentDetailPage />} />
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
                <Route path="mcp-sources" element={<McpSourcesPage />} />
                <Route path="mcp-sources/:id" element={<McpSourceDetailPage />} />
                <Route path="users" element={<UsersPage />} />
                <Route path="roles" element={<RolesPage />} />
                <Route path="api-keys" element={<ApiKeysPage />} />
              </Route>
            </Route>
          </Routes>
        </Suspense>
      </BrowserRouter>
      <AuthModal />
      <Toaster />
    </QueryClientProvider>
  )
}
