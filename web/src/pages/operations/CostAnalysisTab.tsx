import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type UsageSummary = components['schemas']['UsageSummary']
type Period = 'day' | 'week' | 'month'

/** Dependency-free SVG line chart. Two series (tokens / cost) share the
 * frame but get distinct colors AND line styles (solid vs. dashed) per
 * design-system.md's colorblind-safe requirement — color alone never
 * carries the distinction. No brand-gradient fill, per spec-18. */
function TrendChart({ points }: { points: { key: string; tokens: number; cost: number }[] }) {
  if (points.length < 2) {
    return <p className="text-body-sm py-space-8 text-center text-ink-500">数据点太少，暂无法绘制趋势图</p>
  }
  const width = 640
  const height = 200
  const pad = 32
  const maxTokens = Math.max(...points.map((p) => p.tokens), 1)
  const maxCost = Math.max(...points.map((p) => p.cost), 0.01)

  const x = (i: number) => pad + (i / (points.length - 1)) * (width - pad * 2)
  const yTokens = (v: number) => height - pad - (v / maxTokens) * (height - pad * 2)
  const yCost = (v: number) => height - pad - (v / maxCost) * (height - pad * 2)

  const tokensPath = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i)} ${yTokens(p.tokens)}`).join(' ')
  const costPath = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i)} ${yCost(p.cost)}`).join(' ')

  return (
    <div className="rounded-lg border border-border bg-surface p-space-4">
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label="Token 与成本趋势图">
        <line x1={pad} y1={height - pad} x2={width - pad} y2={height - pad} stroke="var(--color-border)" strokeWidth="1" />
        <line x1={pad} y1={pad} x2={pad} y2={height - pad} stroke="var(--color-border)" strokeWidth="1" />
        <path d={tokensPath} fill="none" stroke="var(--color-primary)" strokeWidth="2" />
        <path d={costPath} fill="none" stroke="var(--color-secondary)" strokeWidth="2" strokeDasharray="5 4" />
      </svg>
      <div className="mt-space-3 flex gap-space-5">
        <span className="text-caption flex items-center gap-space-2 text-ink-700">
          <span className="inline-block h-[2px] w-4 bg-primary" /> Token 用量
        </span>
        <span className="text-caption flex items-center gap-space-2 text-ink-700">
          <span
            className="inline-block h-0 w-4 border-t-2 border-dashed"
            style={{ borderColor: 'var(--color-secondary)' }}
          />
          成本 (USD)
        </span>
      </div>
    </div>
  )
}

export function CostAnalysisTab() {
  const [period, setPeriod] = useState<Period>('day')

  const query = useQuery({
    queryKey: ['ops-usage', period, 'day'],
    queryFn: async () =>
      unwrap<UsageSummary>(await apiClient.GET('/usage/me', { params: { query: { period, group_by: 'day' } } })),
  })

  function exportCSV() {
    if (!query.data) return
    const rows = [['key', 'tokens', 'cost_usd', 'run_count'], ...query.data.breakdown.map((b) => [b.key, String(b.tokens), String(b.cost_usd), String(b.run_count)])]
    const csv = rows.map((r) => r.join(',')).join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `cost-report-${query.data.period}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  const points = (query.data?.breakdown ?? []).map((b) => ({ key: b.key ?? '', tokens: b.tokens ?? 0, cost: b.cost_usd ?? 0 }))

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex items-center justify-between">
        <Tabs value={period} onValueChange={(v) => setPeriod(v as Period)}>
          <TabsList>
            <TabsTrigger value="day">日</TabsTrigger>
            <TabsTrigger value="week">周</TabsTrigger>
            <TabsTrigger value="month">月</TabsTrigger>
          </TabsList>
        </Tabs>
        <Button variant="secondary" size="sm" onClick={exportCSV} disabled={!query.data || query.data.breakdown.length === 0}>
          导出
        </Button>
      </div>

      {query.isLoading && (
        <>
          <div className="h-[232px] animate-pulse rounded-lg bg-surface-muted" />
          <ListSkeleton rows={4} />
        </>
      )}
      {query.isError && <ErrorPanel message="成本报表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && (
        <>
          <div className="grid grid-cols-1 gap-space-8 rounded-lg border border-border bg-surface px-space-6 py-space-5 min-[901px]:grid-cols-3">
            <div className="flex flex-col gap-space-1">
              <span className="text-data text-ink-900">{query.data.total_tokens.toLocaleString()}</span>
              <span className="text-caption text-ink-500">Token 总量（{query.data.period}）</span>
            </div>
            <div className="flex flex-col gap-space-1">
              <span className="text-data text-ink-900">${query.data.total_cost_usd.toFixed(2)}</span>
              <span className="text-caption text-ink-500">成本总额</span>
            </div>
            <div className="flex flex-col gap-space-1">
              <span className="text-data text-ink-900">{query.data.run_count ?? 0}</span>
              <span className="text-caption text-ink-500">运行次数</span>
            </div>
          </div>

          {points.length === 0 ? (
            <EmptyState title="还没有运行记录" description="发起一次 Bundle 运行后，这里会显示 Token 与成本趋势。" />
          ) : (
            <>
              <TrendChart points={points} />

              <div className="hidden overflow-x-auto rounded-lg border border-border min-[901px]:block">
                <table className="w-full min-w-[480px] border-collapse">
                  <thead>
                    <tr className="border-b border-border text-left">
                      <th className="text-label-md px-space-4 py-space-3 text-ink-700">日期</th>
                      <th className="text-label-md px-space-4 py-space-3 text-ink-700">Token</th>
                      <th className="text-label-md px-space-4 py-space-3 text-ink-700">成本</th>
                      <th className="text-label-md px-space-4 py-space-3 text-ink-700">运行次数</th>
                    </tr>
                  </thead>
                  <tbody>
                    {query.data.breakdown.map((b) => (
                      <tr key={b.key} className="border-b border-border last:border-0 hover:bg-surface-muted">
                        <td className="text-body-sm px-space-4 py-space-3 font-mono text-ink-700">{b.key}</td>
                        <td className="text-body-md px-space-4 py-space-3 text-ink-900">{(b.tokens ?? 0).toLocaleString()}</td>
                        <td className="text-body-md px-space-4 py-space-3 text-ink-900">${(b.cost_usd ?? 0).toFixed(4)}</td>
                        <td className="text-body-md px-space-4 py-space-3 text-ink-900">{b.run_count}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="flex flex-col gap-space-3 min-[901px]:hidden">
                {query.data.breakdown.map((b) => (
                  <div key={b.key} className="flex flex-col gap-space-2 rounded-lg border border-border bg-surface px-space-4 py-space-3">
                    <span className="font-mono text-body-sm text-ink-900">{b.key}</span>
                    <div className="text-caption flex justify-between text-ink-700">
                      <span>Token：{(b.tokens ?? 0).toLocaleString()}</span>
                      <span>成本：${(b.cost_usd ?? 0).toFixed(4)}</span>
                      <span>运行：{b.run_count ?? 0}</span>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </>
      )}
    </div>
  )
}
