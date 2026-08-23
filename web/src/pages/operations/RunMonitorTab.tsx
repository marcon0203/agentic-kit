import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, CircleDashed, XCircle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type RunSummary = components['schemas']['RunSummary']
type RunStatus = components['schemas']['RunStatus']

const STATUS_META: Record<RunStatus, { label: string; icon: typeof CheckCircle2; className: string }> = {
  running: { label: '运行中', icon: CircleDashed, className: 'bg-primary/10 text-primary' },
  finished: { label: '成功', icon: CheckCircle2, className: 'bg-[color-mix(in_srgb,var(--color-success)_12%,transparent)] text-[var(--color-success)]' },
  failed: { label: '失败', icon: XCircle, className: 'bg-[color-mix(in_srgb,var(--color-error)_12%,transparent)] text-[var(--color-error)]' },
}

function StatusPill({ status }: { status: RunStatus }) {
  const meta = STATUS_META[status]
  const Icon = meta.icon
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-caption ${meta.className}`}>
      <Icon className="size-3.5" />
      {meta.label}
    </span>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-space-1">
      <span className="text-data text-ink-900">{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

export function RunMonitorTab() {
  const [status, setStatus] = useState<'all' | RunStatus>('all')

  const query = useQuery({
    queryKey: ['ops-runs', status],
    queryFn: async () =>
      unwrap<{ items: RunSummary[] }>(
        await apiClient.GET('/runs', {
          params: { query: { status: status === 'all' ? undefined : status, sort: '-created_at', limit: 50 } },
        }),
      ),
  })

  const items = useMemo(() => query.data?.items ?? [], [query.data])

  const metrics = useMemo(() => {
    const today = new Date().toDateString()
    const todays = items.filter((r) => new Date(r.created_at).toDateString() === today)
    const finished = items.filter((r) => r.status === 'finished' || r.status === 'failed')
    const successRate = finished.length ? Math.round((finished.filter((r) => r.status === 'finished').length / finished.length) * 100) : 0
    return { todayCount: todays.length, successRate }
  }, [items])

  return (
    <div className="flex flex-col gap-space-6">
      <div className="grid grid-cols-1 gap-space-8 rounded-lg border border-border bg-surface px-space-6 py-space-5 min-[901px]:grid-cols-3">
        <Metric label="今日运行数" value={String(metrics.todayCount)} />
        <Metric label="成功率" value={`${metrics.successRate}%`} />
        <Metric label="运行总数（近 50 条）" value={String(items.length)} />
      </div>

      <div className="flex items-center gap-space-3">
        <Select value={status} onValueChange={(v) => setStatus(v as typeof status)}>
          <SelectTrigger className="h-9 w-[160px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="running">运行中</SelectItem>
            <SelectItem value="finished">成功</SelectItem>
            <SelectItem value="failed">失败</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {query.isLoading && <ListSkeleton rows={8} />}
      {query.isError && <ErrorPanel message="运行列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && status === 'all' && (
        <EmptyState
          title="还没有运行记录"
          description="从应用中心发起一次 Bundle 运行后，这里会显示它的状态与耗时。"
          action={
            <Button asChild size="sm">
              <Link to="/apps">去应用中心发起一次运行</Link>
            </Button>
          }
        />
      )}
      {query.isSuccess && items.length === 0 && status !== 'all' && (
        <EmptyState
          title="当前条件下没有记录"
          description="换一个状态筛选试试。"
          action={
            <Button variant="secondary" size="sm" onClick={() => setStatus('all')}>
              清除筛选
            </Button>
          }
        />
      )}

      {items.length > 0 && (
        <>
          {/* ≥901px: full table. ≤900px: one card per run (spec-18). */}
          <div className="hidden overflow-x-auto rounded-lg border border-border min-[901px]:block">
            <table className="w-full min-w-[720px] border-collapse">
              <thead>
                <tr className="border-b border-border text-left">
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">Bundle</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">状态</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">开始时间</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">结束时间</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700"></th>
                </tr>
              </thead>
              <tbody>
                {items.map((run) => (
                  <tr key={run.run_id} className="border-b border-border last:border-0 hover:bg-surface-muted">
                    <td className="text-body-md px-space-4 py-space-3 text-ink-900">
                      {run.bundle_ref}
                      <span className="ml-space-2 font-mono text-caption text-ink-500">{run.run_id}</span>
                    </td>
                    <td className="px-space-4 py-space-3">
                      <StatusPill status={run.status} />
                    </td>
                    <td className="text-body-sm px-space-4 py-space-3 font-mono text-ink-700">{fmtTime(run.created_at)}</td>
                    <td className="text-body-sm px-space-4 py-space-3 font-mono text-ink-700">
                      {run.finished_at ? fmtTime(run.finished_at) : '—'}
                    </td>
                    <td className="px-space-4 py-space-3 text-right">
                      {run.status === 'failed' ? (
                        <Link to={`/runs/${run.run_id}`} className="text-body-sm font-medium text-primary hover:underline">
                          查看原因
                        </Link>
                      ) : (
                        <Link to={`/runs/${run.run_id}`} className="text-body-sm font-medium text-ink-700 hover:underline">
                          查看
                        </Link>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="flex flex-col gap-space-3 min-[901px]:hidden">
            {items.map((run) => (
              <div key={run.run_id} className="flex flex-col gap-space-2 rounded-lg border border-border bg-surface px-space-4 py-space-3">
                <div className="flex items-center justify-between">
                  <span className="text-body-md text-ink-900">{run.bundle_ref}</span>
                  <StatusPill status={run.status} />
                </div>
                <span className="font-mono text-caption text-ink-500">{run.run_id}</span>
                <div className="text-caption flex justify-between font-mono text-ink-700">
                  <span>{fmtTime(run.created_at)}</span>
                  <span>{run.finished_at ? fmtTime(run.finished_at) : '—'}</span>
                </div>
                <Link
                  to={`/runs/${run.run_id}`}
                  className={`text-body-sm font-medium hover:underline ${run.status === 'failed' ? 'text-primary' : 'text-ink-700'}`}
                >
                  {run.status === 'failed' ? '查看原因' : '查看'}
                </Link>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
