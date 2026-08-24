import { useSearchParams } from 'react-router-dom'

import { PageHeader, TabRail, TabRailItem } from '@/components/common/Page'
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
      <PageHeader
        eyebrow="OPERATIONS"
        title="运营中心"
        description="运行发生了什么、花了多少钱、谁批准过什么。这里只读，改动都发生在各自的中心里。"
      />

      <TabRail>
        <TabRailItem active={tab === 'monitor'} onClick={() => setTab('monitor')}>
          运行监控
        </TabRailItem>
        <TabRailItem active={tab === 'cost'} onClick={() => setTab('cost')}>
          成本分析
        </TabRailItem>
        <TabRailItem active={tab === 'audit'} onClick={() => setTab('audit')}>
          审计日志
        </TabRailItem>
        {isAdmin && (
          <TabRailItem active={tab === 'moderation'} onClick={() => setTab('moderation')}>
            举报处理
          </TabRailItem>
        )}
      </TabRail>

      {tab === 'monitor' && <RunMonitorTab />}
      {tab === 'cost' && <CostAnalysisTab />}
      {tab === 'audit' && <AuditLogTab />}
      {tab === 'moderation' && isAdmin && <ModerationTab />}
    </div>
  )
}
