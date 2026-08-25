import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Cpu, Users, ShieldCheck } from 'lucide-react'

import { PageHeader } from '@/components/common/Page'
import { SectionSidebar, type SectionSidebarGroup } from '@/components/layout/SectionSidebar'

type Section = 'providers' | 'users' | 'roles'

const SECTION_GROUPS: SectionSidebarGroup[] = [
  {
    label: '模型',
    items: [{ value: 'providers', label: '模型提供商', icon: Cpu }],
  },
  {
    label: '用户与权限',
    items: [
      { value: 'users', label: '用户管理', icon: Users },
      { value: 'roles', label: '角色权限', icon: ShieldCheck },
    ],
  },
]

// No description here on purpose — see AppsLayout's SECTION_TITLE comment:
// a per-item description of varying length made the page jump on switch.
const SECTION_TITLE: Record<Section, string> = {
  providers: '模型提供商',
  users: '用户管理',
  roles: '角色权限',
}

/**
 * 系统配置的外壳，和 AppsLayout 是同一个两级导航模式：顶部横向导航是一级
 * 菜单，这里的左侧栏是二级菜单，每一项各自一条 /settings/<section> 路由。
 */
export function SettingsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const section = (location.pathname.split('/settings/')[1]?.split('/')[0] || 'providers') as Section
  const title = SECTION_TITLE[section] ?? SECTION_TITLE.providers

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="SETTINGS" title={title} />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar groups={SECTION_GROUPS} active={section} onChange={(v) => navigate(`/settings/${v}`)} />

        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
