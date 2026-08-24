import { useQuery } from '@tanstack/react-query'

import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type AuditLog = components['schemas']['AuditLog']

const ACTION_LABELS: Record<string, string> = {
  'human_gate.approved': '审批通过',
  'human_gate.rejected': '审批驳回',
  'moderation.takedown': '管理员下架',
}

function fmtTime(iso: string) {
  // 精确到秒，等宽字体展示 — spec-18 审计日志的特殊要求。
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

export function AuditLogTab() {
  const query = useQuery({
    queryKey: ['ops-audit-logs'],
    queryFn: async () => unwrap<{ items: AuditLog[] }>(await apiClient.GET('/audit-logs', { params: { query: { limit: 50 } } })),
  })

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-6">
      {query.isLoading && <ListSkeleton rows={8} />}
      {query.isError && <ErrorPanel message="审计日志加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyState
          title="还没有需要你决定的事"
          description="你批准或驳回过的每一个 human gate 都会记在这里。记录只增不改，谁也删不掉——包括你自己。"
        />
      )}

      {items.length > 0 && (
        <>
          <div className="hidden overflow-x-auto rounded-lg border border-border min-[901px]:block">
            <table className="w-full min-w-[640px] border-collapse">
              <thead>
                <tr className="border-b border-border text-left">
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">操作类型</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">对象</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700">时间</th>
                  <th className="text-label-md px-space-4 py-space-3 text-ink-700"></th>
                </tr>
              </thead>
              <tbody>
                {items.map((log) => (
                  <tr key={log.id} className="border-b border-border last:border-0 hover:bg-surface-muted">
                    <td className="text-body-md px-space-4 py-space-3 text-ink-900">{ACTION_LABELS[log.action] ?? log.action}</td>
                    <td className="text-ref tabular px-space-4 py-space-3 text-ink-700">
                      {log.target_type}#{log.target_id}
                    </td>
                    <td className="text-ref tabular px-space-4 py-space-3 text-ink-700">{fmtTime(log.created_at)}</td>
                    <td className="px-space-4 py-space-3 text-right">
                      <details>
                        <summary className="text-body-sm cursor-pointer text-blueprint">查看详情</summary>
                        <pre className="text-caption mt-space-2 max-w-[360px] overflow-x-auto rounded-md bg-surface-muted p-space-2 text-left text-ink-700">
                          {JSON.stringify(log.detail, null, 2)}
                        </pre>
                      </details>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="flex flex-col gap-space-3 min-[901px]:hidden">
            {items.map((log) => (
              <div key={log.id} className="flex flex-col gap-space-2 rounded-lg border border-border bg-surface px-space-4 py-space-3">
                <div className="flex items-center justify-between">
                  <span className="text-body-md text-ink-900">{ACTION_LABELS[log.action] ?? log.action}</span>
                  <span className="text-ref text-ink-700">{fmtTime(log.created_at)}</span>
                </div>
                <span className="text-ref text-ink-500">
                  {log.target_type}#{log.target_id}
                </span>
                <details>
                  <summary className="text-body-sm cursor-pointer text-blueprint">查看详情</summary>
                  <pre className="text-caption mt-space-2 overflow-x-auto rounded-md bg-surface-muted p-space-2 text-ink-700">
                    {JSON.stringify(log.detail, null, 2)}
                  </pre>
                </details>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
