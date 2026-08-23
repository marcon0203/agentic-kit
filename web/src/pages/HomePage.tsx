import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type UsageSummary = components['schemas']['UsageSummary']

const QUICK_LINKS = [
  { to: '/apps', title: '应用中心', desc: '创建与运行 Bundle' },
  { to: '/resources', title: '资源中心', desc: '注册 Tool / Skill / MCP / 知识库' },
  { to: '/models', title: '模型中心', desc: '接入模型 Provider' },
  { to: '/marketplace', title: '应用广场', desc: '订阅他人发布的资源' },
]

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-space-1">
      <span className="text-data text-ink-900">{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

export function HomePage() {
  const user = useAuthStore((s) => s.user)

  const usageQuery = useQuery({
    queryKey: ['usage-me', 'month'],
    queryFn: async () =>
      unwrap<UsageSummary>(await apiClient.GET('/usage/me', { params: { query: { period: 'month' } } })),
    enabled: !!user,
  })

  return (
    <div className="flex flex-col gap-space-10">
      <div>
        <p className="text-eyebrow text-primary">AI Agent 平台</p>
        <h1 className="text-headline-lg mt-space-2 text-ink-900">
          {user ? `欢迎回来，${user.display_name}` : '编排、运行、审批你的 Agent Bundle'}
        </h1>
        <p className="text-body-lg mt-space-3 max-w-[720px] text-ink-700">
          从应用广场订阅一个 Bundle，或在应用中心创建自己的编排——Chat 式运行详情页、human gate 审批与运营看板已就绪。
        </p>
      </div>

      {user && (
        <Card>
          <CardHeader>
            <CardTitle className="text-headline-sm">本月用量</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-3 gap-space-6">
            <Metric label="Token 消耗" value={(usageQuery.data?.total_tokens ?? 0).toLocaleString()} />
            <Metric label="成本" value={`$${(usageQuery.data?.total_cost_usd ?? 0).toFixed(4)}`} />
            <Metric label="运行次数" value={(usageQuery.data?.run_count ?? 0).toString()} />
          </CardContent>
        </Card>
      )}

      <div>
        <h2 className="text-headline-sm mb-space-5 text-ink-900">快速入口</h2>
        <div className="grid grid-cols-1 gap-space-5 md:grid-cols-2 lg:grid-cols-4">
          {QUICK_LINKS.map((l) => (
            <Link key={l.to} to={l.to}>
              <Card className="h-full transition-all duration-150 hover:-translate-y-1 hover:shadow-md">
                <CardHeader>
                  <CardTitle className="text-headline-sm">{l.title}</CardTitle>
                </CardHeader>
                <CardContent className="text-body-sm text-ink-700">{l.desc}</CardContent>
              </Card>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}
