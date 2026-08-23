import { useSearchParams } from 'react-router-dom'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { RunMonitorTab } from '@/pages/operations/RunMonitorTab'
import { CostAnalysisTab } from '@/pages/operations/CostAnalysisTab'
import { AuditLogTab } from '@/pages/operations/AuditLogTab'
import { ModerationTab } from '@/pages/operations/ModerationTab'
import { useAuthStore } from '@/lib/auth/store'

type Tab = 'monitor' | 'cost' | 'audit' | 'moderation'

export function OperationsPage() {
  const isAdmin = useAuthStore((s) => s.user?.is_admin ?? false)
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = (searchParams.get('tab') as Tab) || 'monitor'

  function setTab(t: Tab) {
    setSearchParams(t === 'monitor' ? {} : { tab: t })
  }

  return (
    <div className="flex flex-col gap-space-6">
      <h1 className="text-headline-md text-ink-900">运营中心</h1>

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="monitor">监控中心</TabsTrigger>
          <TabsTrigger value="cost">成本分析</TabsTrigger>
          <TabsTrigger value="audit">审计日志</TabsTrigger>
          {isAdmin && <TabsTrigger value="moderation">举报处理</TabsTrigger>}
        </TabsList>
      </Tabs>

      {tab === 'monitor' && <RunMonitorTab />}
      {tab === 'cost' && <CostAnalysisTab />}
      {tab === 'audit' && <AuditLogTab />}
      {tab === 'moderation' && isAdmin && <ModerationTab />}
    </div>
  )
}
