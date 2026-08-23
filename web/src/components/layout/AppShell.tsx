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
  { to: '/settings', label: '系统设置' },
]

export function AppShell() {
  const user = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const openModal = useAuthStore((s) => s.openModal)

  return (
    <div className="min-h-screen bg-surface-page">
      <header className="sticky top-0 z-40 h-16 border-b border-border bg-surface">
        <div className="mx-auto flex h-full max-w-container-app items-center justify-between px-space-6">
          <div className="flex items-center gap-space-8">
            <span className="text-headline-sm shrink-0 text-ink-900">AI Agent 平台</span>
            <nav aria-label="主导航" className="flex items-center gap-space-6">
              {NAV_ITEMS.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    cn(
                      'text-body-md flex h-10 items-center border-b-2 border-transparent px-1 text-tab-text transition-colors duration-150',
                      'focus-visible:ring-ring/50 focus-visible:rounded-xs focus-visible:outline-none focus-visible:ring-[3px]',
                      isActive && 'border-tab-active font-medium text-tab-active',
                    )
                  }
                >
                  {item.label}
                </NavLink>
              ))}
              <NavLink
                to="/marketplace"
                className={({ isActive }) =>
                  cn(
                    'text-body-md flex h-10 items-center border-b-2 border-transparent px-1 text-tab-text transition-colors duration-150',
                    'focus-visible:ring-ring/50 focus-visible:rounded-xs focus-visible:outline-none focus-visible:ring-[3px]',
                    isActive && 'border-tab-active font-medium text-tab-active',
                  )
                }
              >
                应用广场
              </NavLink>
            </nav>
          </div>

          <div className="flex items-center gap-space-3">
            {user ? (
              <>
                <div
                  aria-hidden
                  className="flex size-9 items-center justify-center rounded-full bg-surface-muted text-body-sm font-medium text-ink-700"
                >
                  {user.display_name.slice(0, 1).toUpperCase()}
                </div>
                <span className="text-body-sm text-ink-700">{user.display_name}</span>
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

      <main className="mx-auto max-w-container-app px-space-6 py-space-10">
        <Outlet />
      </main>
    </div>
  )
}
