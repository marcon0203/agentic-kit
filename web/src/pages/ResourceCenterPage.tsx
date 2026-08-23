import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { RegisterResourceDialog } from '@/components/resources/RegisterResourceDialog'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ResourceType = components['schemas']['ResourceType']
type Resource = components['schemas']['Resource']

const TABS: { value: ResourceType; label: string }[] = [
  { value: 'tool', label: 'Tool' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp', label: 'MCP Server' },
  { value: 'knowledge_base', label: '知识库' },
]

export function ResourceCenterPage() {
  const [type, setType] = useState<ResourceType>('tool')
  const [search, setSearch] = useState('')
  const [registerOpen, setRegisterOpen] = useState(false)
  const [toggleError, setToggleError] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['resources', type],
    queryFn: async () =>
      unwrap<{ items: Resource[]; has_more: boolean }>(
        await apiClient.GET('/resources', { params: { query: { type } } }),
      ),
  })

  async function toggleStatus(r: Resource) {
    setToggleError(null)
    const nextStatus = r.status === 1 ? 2 : 1
    try {
      unwrap(
        await apiClient.PATCH('/resources/{id}', { params: { path: { id: r.id } }, body: { status: nextStatus } }),
      )
      queryClient.invalidateQueries({ queryKey: ['resources', type] })
    } catch (err) {
      setToggleError(err instanceof ApiError ? err.message : '操作失败，请稍后重试')
    }
  }

  const items = query.data?.items ?? []
  const filtered = search
    ? items.filter((r) => r.ref.includes(search) || (r.display_name ?? '').includes(search))
    : items

  return (
    <div className="flex flex-col gap-space-6">
      <h1 className="text-headline-md text-ink-900">资源中心</h1>

      <Tabs value={type} onValueChange={(v) => setType(v as ResourceType)}>
        <TabsList>
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="flex items-center gap-space-3">
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索 ref 或名称"
          className="max-w-xs"
        />
        <Button className="ml-auto" onClick={() => setRegisterOpen(true)}>
          注册资源
        </Button>
      </div>

      {toggleError && (
        <p role="alert" className="text-body-sm text-[var(--color-error)]">
          {toggleError}
        </p>
      )}

      {query.isLoading && <ListSkeleton />}

      {query.isError && <ErrorPanel message="资源列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && filtered.length === 0 && items.length === 0 && (
        <EmptyState
          title="还没有注册任何资源"
          description="资源是 Agent 能力白名单可以引用的工具、技能、MCP Server 或知识库。"
          action={
            <Button size="sm" onClick={() => setRegisterOpen(true)}>
              注册第一个资源
            </Button>
          }
        />
      )}

      {query.isSuccess && filtered.length === 0 && items.length > 0 && (
        <EmptyState
          title="没有找到匹配的资源"
          description="换一个关键词试试。"
          action={
            <Button variant="secondary" size="sm" onClick={() => setSearch('')}>
              清除筛选条件
            </Button>
          }
        />
      )}

      {filtered.length > 0 && (
        <ul className="flex flex-col gap-space-2">
          {filtered.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4"
            >
              <div className="flex-1">
                <div className="flex items-center gap-space-2">
                  <span className="font-mono text-body-md text-ink-900">{r.ref}</span>
                  {r.display_name && <span className="text-body-sm text-ink-500">{r.display_name}</span>}
                </div>
                {r.health && r.health !== 'unknown' && (
                  <span className={r.health === 'healthy' ? 'text-caption text-[var(--color-success)]' : 'text-caption text-[var(--color-error)]'}>
                    {r.health === 'healthy' ? '连接正常' : '连接异常'}
                  </span>
                )}
              </div>
              <Badge variant={r.status === 1 ? 'default' : 'secondary'}>{r.status === 1 ? '已启用' : '已停用'}</Badge>
              <Button variant="secondary" size="sm" onClick={() => toggleStatus(r)}>
                {r.status === 1 ? '停用' : '启用'}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <RegisterResourceDialog
        type={type}
        open={registerOpen}
        onOpenChange={setRegisterOpen}
        onCreated={() => queryClient.invalidateQueries({ queryKey: ['resources', type] })}
      />
    </div>
  )
}
