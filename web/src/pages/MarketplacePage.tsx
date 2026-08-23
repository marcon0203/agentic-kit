import { useSearchParams } from 'react-router-dom'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
      {isAuthenticated && (
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
          <TabsList>
            <TabsTrigger value="browse">广场浏览</TabsTrigger>
            <TabsTrigger value="publish">我的发布</TabsTrigger>
            <TabsTrigger value="subscriptions">我的订阅</TabsTrigger>
          </TabsList>
        </Tabs>
      )}

      {tab === 'browse' && <MarketplaceBrowsePage />}
      {tab === 'publish' && isAuthenticated && <MyListingsPage />}
      {tab === 'subscriptions' && isAuthenticated && <MySubscriptionsPage />}
    </div>
  )
}
