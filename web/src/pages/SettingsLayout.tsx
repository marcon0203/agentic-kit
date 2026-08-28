import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Cpu, Globe, Users, ShieldCheck } from 'lucide-react'

import { SectionSidebar, type SectionSidebarGroup } from '@/components/layout/SectionSidebar'

type Section = 'providers' | 'users' | 'roles' | 'skill-sources'

const SECTION_GROUPS: SectionSidebarGroup[] = [
  {
    label: '模型',
    items: [{ value: 'providers', label: '模型提供商', icon: Cpu }],
  },
  {
    label: '资源',
    items: [{ value: 'skill-sources', label: 'Skill 源', icon: Globe }],
  },
  {
    label: '用户与权限',
    items: [
      { value: 'users', label: '用户管理', icon: Users },
      { value: 'roles', label: '角色权限', icon: ShieldCheck },
    ],
  },
]

/**
 * 系统配置的外壳，和 AppsLayout 是同一个两级导航模式：顶部横向导航是一级
 * 菜单，这里的左侧栏是二级菜单，每一项各自一条 /settings/<section> 路由。
 * 侧栏已高亮当前分区，页面不再重复渲染分区名页头。
 */
export function SettingsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const section = (location.pathname.split('/settings/')[1]?.split('/')[0] || 'providers') as Section

  return (
    <div className="flex flex-1 flex-col gap-space-6 sm:flex-row">
      <SectionSidebar groups={SECTION_GROUPS} active={section} onChange={(v) => navigate(`/settings/${v}`)} />

      <div className="min-w-0 flex-1 rounded-lg border border-border bg-surface p-space-6">
        <Outlet />
      </div>
    </div>
  )
}
