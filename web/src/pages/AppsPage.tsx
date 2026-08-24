import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Store, LayoutGrid, Upload, Heart } from 'lucide-react'

import { PageHeader, TabRail, TabRailItem } from '@/components/common/Page'
import { SectionSidebar, type SectionSidebarItem } from '@/components/layout/SectionSidebar'
import { MarketplaceBrowsePage } from '@/pages/MarketplaceBrowsePage'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'
import { MyListingsPage } from '@/pages/MyListingsPage'
import { MySubscriptionsPage } from '@/pages/MySubscriptionsPage'
import { useAuthStore } from '@/lib/auth/store'

type Section = 'browse' | 'manage' | 'publish' | 'subscriptions'

const SECTIONS: SectionSidebarItem[] = [
  { value: 'browse', label: '应用广场', icon: Store },
  { value: 'manage', label: '应用管理', icon: LayoutGrid },
  { value: 'publish', label: '我的发布', icon: Upload },
  { value: 'subscriptions', label: '我的订阅', icon: Heart },
]

const SECTION_COPY: Record<Section, { title: string; description: string }> = {
  browse: {
    title: '应用广场',
    description: '所有人发布的 Bundle、Agent、Skill 与 MCP Server。订阅锁定版本，作者的编排图和提示词不会跟着过来。',
  },
  manage: {
    title: '应用管理',
    description: '这里只看得到你自己创建的 Agent 与 Bundle。Agent 是单个角色的定义，Bundle 把多个 Agent 编排成一次协作，运行从这里发起。',
  },
  publish: {
    title: '我的发布',
    description: '把自己的 Bundle 或 Agent 发布到广场，让其他人可以订阅使用；广场上看到的仍然是黑盒，编排图与提示词不会带出去。',
  },
  subscriptions: {
    title: '我的订阅',
    description: '订阅版本已锁定，作者发布新版本时会单独提醒，是否升级由你决定。',
  },
}

/**
 * 应用广场（发布市场）与应用中心（自己的 Bundle/Agent 编排）合并成一个中心：
 * 顶部横向导航是一级菜单，这里的左侧栏是二级菜单——同一个"应用"概念下，
 * 广场看别人发的，管理看自己建的，互为镜像，拆成两个顶级中心反而让人来回切。
 */
export function AppsPage() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const [searchParams, setSearchParams] = useSearchParams()
  const section = ((searchParams.get('tab') as Section) || 'browse') as Section
  const [manageTab, setManageTab] = useState<'bundle' | 'agent'>('bundle')

  function setSection(next: Section) {
    setSearchParams(next === 'browse' ? {} : { tab: next })
  }

  const copy = SECTION_COPY[section]

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="APPLICATIONS" title={copy.title} description={copy.description} />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar items={SECTIONS} active={section} onChange={(v) => setSection(v as Section)} />

        <div className="min-w-0 flex-1">
          {section === 'browse' && <MarketplaceBrowsePage />}

          {section === 'manage' && (
            <div className="flex flex-col gap-space-6">
              <TabRail>
                <TabRailItem active={manageTab === 'bundle'} onClick={() => setManageTab('bundle')}>
                  Bundle 编排
                </TabRailItem>
                <TabRailItem active={manageTab === 'agent'} onClick={() => setManageTab('agent')}>
                  Agent 定义
                </TabRailItem>
              </TabRail>
              {manageTab === 'bundle' ? <BundleListPage /> : <AgentDefinitionPage />}
            </div>
          )}

          {section === 'publish' && isAuthenticated && <MyListingsPage />}
          {section === 'subscriptions' && isAuthenticated && <MySubscriptionsPage />}
        </div>
      </div>
    </div>
  )
}
