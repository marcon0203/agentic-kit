import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { PageHeader, Ref, TabRail, TabRailItem } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { RegisterResourceDialog } from '@/components/resources/RegisterResourceDialog'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type ResourceType = components['schemas']['ResourceType']
type Resource = components['schemas']['Resource']

/* Each kind gets its own empty-state copy. "还没有资源" is the same sentence
   four times over and helps nobody; what a person needs to know is what this
   particular kind is for and what registering one would let them do. */
const KINDS: {
  value: ResourceType
  label: string
  blank: { title: string; description: string; cta: string }
}[] = [
  {
    value: 'tool',
    label: 'Tool',
    blank: {
      title: '给 Agent 一件能用的工具',
      description:
        'Tool 是 Agent 能调用的外部能力：一个检索接口、一个内部服务。注册后才能写进 Agent 的能力白名单。',
      cta: '注册 Tool',
    },
  },
  {
    value: 'skill',
    label: 'Skill',
    blank: {
      title: '沉淀一段可复用的做法',
      description: 'Skill 把一段固定的做事方式打包，让多个 Agent 共用同一套步骤，而不是各写各的提示词。',
      cta: '注册 Skill',
    },
  },
  {
    value: 'mcp',
    label: 'MCP Server',
    blank: {
      title: '接入一台 MCP Server',
      description:
        '登记地址与凭证后平台会立刻探测一次连通性，结果显示在这里。凭证加密落库，任何响应都不会带出来。',
      cta: '接入 MCP Server',
    },
  },
  {
    value: 'knowledge_base',
    label: '知识库',
    blank: {
      title: '让 Agent 有资料可查',
      description: '知识库登记后可以被 Agent 引用，回答时从这里检索，而不是全靠模型自己记得。',
      cta: '注册知识库',
    },
  },
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
        await apiClient.PATCH('/resources/{id}', {
          params: { path: { id: r.id } },
          body: { status: nextStatus },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['resources', type] })
    } catch (err) {
      setToggleError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  const kind = KINDS.find((k) => k.value === type)!
  const items = query.data?.items ?? []
  const filtered = search
    ? items.filter((r) => r.ref.includes(search) || (r.display_name ?? '').includes(search))
    : items

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader
        eyebrow="RESOURCES"
        title="资源中心"
        description="Agent 能引用的一切都先在这里登记。凭证加密落库，注册之后任何接口都不会再把它读出来。"
        actions={<Button onClick={() => setRegisterOpen(true)}>{kind.blank.cta}</Button>}
      />

      <TabRail>
        {KINDS.map((k) => (
          <TabRailItem key={k.value} active={k.value === type} onClick={() => setType(k.value)}>
            {k.label}
          </TabRailItem>
        ))}
      </TabRail>

      {items.length > 0 && (
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="按 ref 或名称筛选"
          className="max-w-xs"
        />
      )}

      {toggleError && (
        <p role="alert" className="text-body-sm text-rust">
          {toggleError}
        </p>
      )}

      {query.isLoading && <ListSkeleton />}

      {query.isError && <ErrorPanel message="资源列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyRail
          title={kind.blank.title}
          description={kind.blank.description}
          action={
            <Button size="sm" onClick={() => setRegisterOpen(true)}>
              {kind.blank.cta}
            </Button>
          }
        />
      )}

      {query.isSuccess && items.length > 0 && filtered.length === 0 && (
        <EmptyRail
          title={`没有 ref 或名称包含「${search}」的${kind.label}`}
          description="筛选只匹配 ref 和显示名称，不搜索配置内容。"
          action={
            <Button variant="outline" size="sm" onClick={() => setSearch('')}>
              清除筛选
            </Button>
          }
        />
      )}

      {filtered.length > 0 && (
        <ul className="overflow-hidden rounded-lg border border-border bg-surface">
          {filtered.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0"
            >
              <span
                aria-hidden
                className={cn(
                  'size-2 shrink-0 rounded-full',
                  r.status === 1 ? 'bg-moss' : 'bg-border-strong',
                )}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-center gap-space-3">
                  <Ref>{r.ref}</Ref>
                  {r.display_name && (
                    <span className="text-body-sm truncate text-ink-700">{r.display_name}</span>
                  )}
                </span>
                {r.health && r.health !== 'unknown' && (
                  <span
                    className={cn(
                      'text-caption',
                      r.health === 'healthy' ? 'text-moss' : 'text-rust',
                    )}
                  >
                    {r.health === 'healthy' ? '上次探测：连接正常' : '上次探测：连不上，检查地址与凭证'}
                  </span>
                )}
              </span>
              <span
                className={cn(
                  'text-caption w-12 shrink-0 text-right',
                  r.status === 1 ? 'text-moss' : 'text-ink-500',
                )}
              >
                {r.status === 1 ? '已启用' : '已停用'}
              </span>
              <Button variant="outline" size="sm" onClick={() => toggleStatus(r)}>
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
