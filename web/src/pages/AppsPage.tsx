import { useState } from 'react'

import { PageHeader, TabRail, TabRailItem } from '@/components/common/Page'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'

/**
 * The app centre owns the masthead and the tab rail; the two views below
 * render only their own content. Previously each view drew its own <h1>,
 * which put a page title *underneath* the tabs that switched between them.
 */
export function AppsPage() {
  const [tab, setTab] = useState<'bundle' | 'agent'>('bundle')

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader
        eyebrow="APPLICATIONS"
        title="应用中心"
        description="Agent 是单个角色的定义，Bundle 把多个 Agent 编排成一次协作。运行从这里发起。"
      />

      <TabRail>
        <TabRailItem active={tab === 'bundle'} onClick={() => setTab('bundle')}>
          Bundle 编排
        </TabRailItem>
        <TabRailItem active={tab === 'agent'} onClick={() => setTab('agent')}>
          Agent 定义
        </TabRailItem>
      </TabRail>

      {tab === 'bundle' ? <BundleListPage /> : <AgentDefinitionPage />}
    </div>
  )
}
