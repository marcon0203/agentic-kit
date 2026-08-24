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

const SECTION_COPY: Record<Section, { title: string; description: string }> = {
  providers: {
    title: '模型提供商',
    description:
      '系统级配置：先登记一个 Provider（名称、图标），再在它下面逐个添加可用的模型（如 deepseek-v3），标注类型（文本/图片/视频/向量等）。启用后会出现在模型广场供所有人浏览。',
  },
  users: {
    title: '用户管理',
    description: '启用/停用账号，把角色分配给具体的人——一个人能做什么由他持有的角色决定。',
  },
  roles: {
    title: '角色权限',
    description:
      '标准 RBAC：一个角色是一组权限，每个权限对应界面上的一个具体按钮/操作。新增角色、勾选它能做什么，再去用户管理页分配给人。',
  },
}

/**
 * 系统配置的外壳，和 AppsLayout 是同一个两级导航模式：顶部横向导航是一级
 * 菜单，这里的左侧栏是二级菜单，每一项各自一条 /settings/<section> 路由。
 */
export function SettingsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const section = (location.pathname.split('/settings/')[1]?.split('/')[0] || 'providers') as Section
  const copy = SECTION_COPY[section] ?? SECTION_COPY.providers

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="SETTINGS" title={copy.title} description={copy.description} />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar groups={SECTION_GROUPS} active={section} onChange={(v) => navigate(`/settings/${v}`)} />

        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
