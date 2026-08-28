import { NavLink, Outlet } from 'react-router-dom'
import { BarChart3, ChevronDown, Cpu, Home, LogOut, Settings, Store } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAuthStore } from '@/lib/auth/store'

/**
 * Navigation is drawn as stations on a rule, matching the run rail the rest
 * of the app is built around: the active item sits on the line with a mark
 * under it, rather than in a filled pill. A pill reads as a button and
 * invites a click on the page you are already looking at.
 */
const NAV_ITEMS = [
  { to: '/', label: '首页', icon: Home, end: true },
  { to: '/apps', label: '应用广场', icon: Store },
  { to: '/models', label: '模型广场', icon: Cpu },
  { to: '/ops', label: '运营中心', icon: BarChart3 },
  { to: '/settings', label: '系统设置', icon: Settings },
]

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'text-body-sm relative flex h-full shrink-0 items-center gap-space-2 px-0.5 transition-colors duration-150',
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
        {/* 通栏：不受 max-w-container-app 约束，logo 贴最左、用户信息贴最右，
            菜单在两侧等宽弹性区之间居中；min-w-fit 保证窄屏时两边不被压没。 */}
        <div className="flex h-14 items-stretch px-space-6">
          <div className="flex min-w-fit flex-1 items-center">
            <NavLink to="/" className="flex shrink-0 items-center gap-space-2">
              <span aria-hidden className="size-2 rounded-full bg-signal" />
              <span className="text-display-sm hidden tracking-tight text-ink-900 sm:inline">
                Agentic Kit
              </span>
            </NavLink>
          </div>

          <nav
            aria-label="主导航"
            className="flex min-w-0 items-stretch gap-space-5 overflow-x-auto"
          >
            {NAV_ITEMS.map((item) => {
              const Icon = item.icon
              return (
                <NavLink key={item.to} to={item.to} end={item.end} className={navLinkClass}>
                  <Icon className="size-4" aria-hidden />
                  {item.label}
                </NavLink>
              )
            })}
          </nav>

          <div className="flex min-w-fit flex-1 items-center justify-end gap-space-3">
            {user ? (
              <DropdownMenu>
                <DropdownMenuTrigger
                  className={cn(
                    'flex items-center gap-space-2 rounded-lg py-1.5 pr-space-2 pl-1.5 transition-colors',
                    'hover:bg-surface-muted outline-none focus-visible:ring-ring/50 focus-visible:ring-[3px]',
                  )}
                >
                  <span
                    aria-hidden
                    className="text-caption flex size-7 items-center justify-center rounded-full bg-surface-muted font-medium text-ink-700"
                  >
                    {user.display_name.slice(0, 1).toUpperCase()}
                  </span>
                  <span className="text-body-sm hidden text-ink-700 sm:inline">
                    {user.display_name}
                  </span>
                  <ChevronDown className="size-4 text-ink-500" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-[10rem]">
                  <DropdownMenuLabel>{user.display_name}</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => clearSession()}>
                    <LogOut aria-hidden />
                    退出登录
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              <Button size="sm" onClick={() => openModal('manual')}>
                登录
              </Button>
            )}
          </div>
        </div>
      </header>

      {/* flex 列 + 子页面根节点 flex-1：把 main 的高度传下去，二级布局
          （如应用中心侧栏）才能拉伸到整个内容区高度，右边的分隔线才贯穿。 */}
      <main id="main" className="flex w-full flex-1 flex-col px-space-6 pt-space-6 pb-space-8">
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
