import type { ReactNode } from 'react'
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Activity, Coins, ScrollText, Flag, ShieldCheck } from 'lucide-react'

import { SectionSidebar, type SectionSidebarItem } from '@/components/layout/SectionSidebar'
import { useAuthStore } from '@/lib/auth/store'

type Section = 'monitor' | 'cost' | 'audit' | 'moderation' | 'plugin-moderation'

/** 举报处理 / 插件审核只对管理员开放；非管理员直链进来时回到运行监控。 */
function RequireAdmin({ children }: { children: ReactNode }) {
  const isAdmin = useAuthStore((s) => s.user?.is_admin ?? false)
  return isAdmin ? <>{children}</> : <Navigate to="/ops/monitor" replace />
}

/**
 * 运营中心的外壳，和 AppsLayout / SettingsLayout 同一个两级导航模式：顶部
 * 横向导航是一级菜单，这里的左侧栏是二级菜单，每一项各自一条
 * /ops/<section> 路由。原来是单页 + TabRail，和其他中心不统一。
 */
export function OpsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const isAdmin = useAuthStore((s) => s.user?.is_admin ?? false)
  const section = ((location.pathname.split('/ops/')[1] ?? '').split('/')[0] || 'monitor') as Section

  const items: SectionSidebarItem[] = [
    { value: 'monitor', label: '运行监控', icon: Activity },
    { value: 'cost', label: '成本分析', icon: Coins },
    { value: 'audit', label: '审计日志', icon: ScrollText },
    ...(isAdmin
      ? [
          { value: 'moderation', label: '举报处理', icon: Flag },
          { value: 'plugin-moderation', label: '插件审核', icon: ShieldCheck },
        ]
      : []),
  ]

  return (
    <div className="flex flex-1 flex-col gap-space-6 sm:flex-row">
      <SectionSidebar items={items} active={section} onChange={(v) => navigate(`/ops/${v}`)} />

      {/* 内容区做成一张浮在页面底色上的卡片；侧栏裸露在卡片外，无边框。 */}
      <div className="min-w-0 flex-1 rounded-lg border border-border bg-surface p-space-6">
        <Outlet />
      </div>
    </div>
  )
}

export { RequireAdmin }
