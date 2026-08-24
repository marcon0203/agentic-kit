import { NavLink, Outlet } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/lib/auth/store'

/**
 * Navigation is drawn as stations on a rule, matching the run rail the rest
 * of the app is built around: the active item sits on the line with a mark
 * under it, rather than in a filled pill. A pill reads as a button and
 * invites a click on the page you are already looking at.
 */
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
    'text-body-sm relative flex h-full shrink-0 items-center px-0.5 transition-colors duration-150',
    'after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:transition-colors',
    isActive
      ? 'font-medium text-ink-900 after:bg-blueprint'
      : 'text-ink-500 after:bg-transparent hover:text-ink-900',
  )

export function AppShell() {
  const user = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const openModal = useAuthStore((s) => s.openModal)

  return (
    <div className="flex min-h-screen flex-col bg-surface-page">
      <a
        href="#main"
        className="text-label-md sr-only focus:not-sr-only focus:absolute focus:left-space-4 focus:top-space-4 focus:z-50 focus:rounded-sm focus:bg-blueprint focus:px-space-4 focus:py-space-2 focus:text-white"
      >
        跳到主要内容
      </a>

      <header className="sticky top-0 z-40 border-b border-border bg-surface-page/92 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-container-app items-stretch gap-space-8 px-space-6">
          <NavLink to="/" className="flex shrink-0 items-center gap-space-2">
            <span aria-hidden className="size-2 rounded-full bg-signal" />
            <span className="text-display-sm hidden tracking-tight text-ink-900 sm:inline">
              Agentic Kit
            </span>
          </NavLink>

          <nav
            aria-label="主导航"
            className="flex min-w-0 flex-1 items-stretch gap-space-5 overflow-x-auto"
          >
            {NAV_ITEMS.map((item) => (
              <NavLink key={item.to} to={item.to} end={item.end} className={navLinkClass}>
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="flex shrink-0 items-center gap-space-3">
            {user ? (
              <>
                <span
                  aria-hidden
                  className="text-caption flex size-7 items-center justify-center rounded-full bg-surface-muted font-medium text-ink-700"
                >
                  {user.display_name.slice(0, 1).toUpperCase()}
                </span>
                <span className="text-body-sm hidden text-ink-700 sm:inline">
                  {user.display_name}
                </span>
                <Button variant="ghost" size="sm" onClick={() => clearSession()}>
                  退出登录
                </Button>
              </>
            ) : (
              <Button size="sm" onClick={() => openModal('manual')}>
                登录
              </Button>
            )}
          </div>
        </div>
      </header>

      <main id="main" className="mx-auto w-full max-w-container-app flex-1 px-space-6 py-space-8">
        <Outlet />
      </main>

      <footer className="border-t border-border">
        <div className="text-caption mx-auto flex max-w-container-app flex-wrap items-center justify-between gap-space-3 px-space-6 py-space-5 text-ink-500">
          <span>Agentic Kit · Agent 编排与运行平台</span>
          <span className="text-ref text-ink-500">v1</span>
        </div>
      </footer>
    </div>
  )
}
