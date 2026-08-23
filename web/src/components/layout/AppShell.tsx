import { NavLink, Outlet } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/lib/auth/store'

const NAV_ITEMS = [
  { to: '/', label: '首页', end: true },
  { to: '/apps', label: '应用中心' },
  { to: '/resources', label: '资源中心' },
  { to: '/models', label: '模型中心' },
  { to: '/ops', label: '运营中心' },
  { to: '/marketplace', label: '应用广场' },
  { to: '/settings', label: '系统设置' },
]

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'text-body-sm flex h-9 shrink-0 items-center rounded-full px-3.5 text-tab-text transition-colors duration-150',
    'hover:bg-surface-muted hover:text-ink-900',
    'focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
    isActive && 'bg-primary/10 font-medium text-tab-active hover:bg-primary/10 hover:text-tab-active',
  )

export function AppShell() {
  const user = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const openModal = useAuthStore((s) => s.openModal)

  return (
    <div className="min-h-screen bg-surface-page">
      <header className="sticky top-0 z-40 border-b border-border bg-surface/95 backdrop-blur">
        <div className="mx-auto flex h-16 max-w-container-app items-center gap-space-6 px-space-6">
          <span className="text-label-md shrink-0 tracking-tight text-ink-900">AI Agent 平台</span>

          <nav aria-label="主导航" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
            {NAV_ITEMS.map((item) => (
              <NavLink key={item.to} to={item.to} end={item.end} className={navLinkClass}>
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="flex shrink-0 items-center gap-space-3">
            {user ? (
              <>
                <div
                  aria-hidden
                  className="flex size-8 items-center justify-center rounded-full bg-surface-muted text-caption font-medium text-ink-700"
                >
                  {user.display_name.slice(0, 1).toUpperCase()}
                </div>
                <span className="text-body-sm hidden text-ink-700 sm:inline">{user.display_name}</span>
                <Button variant="ghost" size="sm" onClick={() => clearSession()}>
                  退出登录
                </Button>
              </>
            ) : (
              <Button size="sm" onClick={() => openModal('manual')}>
                登录 / 注册
              </Button>
            )}
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-container-app px-space-6 py-space-8">
        <Outlet />
      </main>
    </div>
  )
}
