import { useState } from 'react'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'

export function AppsPage() {
  const [tab, setTab] = useState<'bundle' | 'agent'>('bundle')

  return (
    <div className="flex flex-col gap-space-6">
      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
        <TabsList>
          <TabsTrigger value="bundle">Bundle</TabsTrigger>
          <TabsTrigger value="agent">Agent 定义</TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === 'bundle' ? <BundleListPage /> : <AgentDefinitionPage />}
    </div>
  )
}
