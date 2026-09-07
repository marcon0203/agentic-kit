import { NavLink, Outlet } from 'react-router-dom'
import { BarChart3, ChevronDown, Cpu, Home, LogOut, Settings, Store, Waypoints } from 'lucide-react'
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

export function AppShell() {
  const user = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const openModal = useAuthStore((s) => s.openModal)

  return (
    // h-screen 而不是 min-h-screen：后者只是**最小**高度，内容一多整个外壳
    // 就跟着长高。那样一来 main 的 flex-1 没有剩余空间可分，等于内容高度，
    // 子页面的 min-h-0 也就无从生效——聊天页的表现是消息把输入框一路顶出
    // 视口（实测 800px 视口下输入框跑到 4142px），而不是消息区自己滚动。
    //
    // 外壳固定一屏、overflow-hidden 兜住，滚动交给下面的 main：这样
    // "框架高度不变、内容滚动"对每个子页面都成立，不用各自去凑高度。
    <div className="flex h-screen flex-col overflow-hidden bg-surface-page">
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
              <span
                aria-hidden
                className="flex size-7 items-center justify-center rounded-md bg-primary text-white shadow-[0_4px_12px_rgb(124_92_252_/_0.35)]"
              >
                <Waypoints className="size-4" />
              </span>
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
                <NavLink key={item.to} to={item.to} end={item.end}>
                  {({ isActive }) => (
                    <span
                      className={cn(
                        'text-body-sm relative flex h-full shrink-0 items-center gap-space-2 px-0.5 transition-colors duration-150',
                        'after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:transition-colors',
                        isActive
                          ? 'font-medium text-ink-900 after:bg-blueprint'
                          : 'text-ink-500 after:bg-transparent hover:text-ink-900',
                      )}
                    >
                      {/* 激活站点的图标也上车：下划线之外再给一个色彩信号，扫一眼就能定位。 */}
                      <Icon className={cn('size-4', isActive ? 'text-blueprint' : '')} aria-hidden />
                      {item.label}
                    </span>
                  )}
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
                    className="text-caption flex size-7 items-center justify-center rounded-full bg-blueprint-tint font-medium text-violet"
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
          （如应用中心侧栏）才能拉伸到整个内容区高度，右边的分隔线才贯穿。

          overflow-y-auto：外壳固定一屏之后，页面滚动条落在这里。普通的长
          页面（广场、设置）照常滚，只是滚的是 main 而不是 window；顶栏在
          main 之外，因此天然常驻，不再依赖 sticky。而聊天这类"自己管滚动"
          的页面把高度用满、内部滚，main 就不会出现滚动条。 */}
      <main id="main" className="flex min-h-0 w-full flex-1 flex-col overflow-y-auto px-space-6 pt-space-6 pb-space-8">
        <Outlet />
      </main>
    </div>
  )
}
