import { useSearchParams } from 'react-router-dom'

import { PageHeader, TabRail, TabRailItem } from '@/components/common/Page'
import { MarketplaceBrowsePage } from '@/pages/MarketplaceBrowsePage'
import { MyListingsPage } from '@/pages/MyListingsPage'
import { MySubscriptionsPage } from '@/pages/MySubscriptionsPage'
import { useAuthStore } from '@/lib/auth/store'

type Tab = 'browse' | 'publish' | 'subscriptions'

export function MarketplacePage() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = (searchParams.get('tab') as Tab) || 'browse'

  function setTab(t: Tab) {
    setSearchParams(t === 'browse' ? {} : { tab: t })
  }

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader
        eyebrow="MARKETPLACE"
        title="应用广场"
        description="订阅别人做好的 Bundle、Agent、Skill 与 MCP Server。订阅锁定版本，作者的编排图和提示词不会跟着过来。"
      />

      {isAuthenticated && (
        <TabRail>
          <TabRailItem active={tab === 'browse'} onClick={() => setTab('browse')}>
            广场浏览
          </TabRailItem>
          <TabRailItem active={tab === 'publish'} onClick={() => setTab('publish')}>
            我的发布
          </TabRailItem>
          <TabRailItem active={tab === 'subscriptions'} onClick={() => setTab('subscriptions')}>
            我的订阅
          </TabRailItem>
        </TabRail>
      )}

      {tab === 'browse' && <MarketplaceBrowsePage />}
      {tab === 'publish' && isAuthenticated && <MyListingsPage />}
      {tab === 'subscriptions' && isAuthenticated && <MySubscriptionsPage />}
    </div>
  )
}
