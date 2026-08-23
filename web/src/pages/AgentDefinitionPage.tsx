import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { AgentForm, definitionToFormState, type FormState } from '@/components/agents/AgentForm'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Agent = components['schemas']['Agent']

export function AgentDefinitionPage() {
  const [mode, setMode] = useState<'list' | 'create'>('list')
  const [copyFrom, setCopyFrom] = useState<FormState | undefined>(undefined)
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['agents'],
    queryFn: async () => unwrap<{ items: Agent[] }>(await apiClient.GET('/agents', {})),
    enabled: mode === 'list',
  })

  if (mode === 'create') {
    return (
      <div className="flex flex-col gap-space-6">
        <div className="flex items-center justify-between">
          <h1 className="text-headline-md text-ink-900">{copyFrom ? '复制 Agent' : '新建 Agent'}</h1>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setMode('list')
              setCopyFrom(undefined)
            }}
          >
            返回列表
          </Button>
        </div>
        <AgentForm
          initial={copyFrom}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: ['agents'] })
            setMode('list')
            setCopyFrom(undefined)
          }}
        />
      </div>
    )
  }

  const items = query.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex items-center justify-between">
        <h1 className="text-headline-md text-ink-900">Agent 定义</h1>
        <Button onClick={() => setMode('create')}>新建 Agent</Button>
      </div>

      {query.isLoading && <ListSkeleton />}
      {query.isError && <ErrorPanel message="Agent 列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyState
          title="还没有创建任何 Agent"
          description="Agent 定义模型、角色设定与能力白名单，是编排 Bundle 的基本单元。"
          action={<Button size="sm" onClick={() => setMode('create')}>创建第一个 Agent</Button>}
        />
      )}

      {items.length > 0 && (
        <ul className="flex flex-col gap-space-2">
          {items.map((a) => (
            <li key={a.id} className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4">
              <div className="flex-1">
                <div className="flex items-center gap-space-2">
                  <span className="font-mono text-body-md text-ink-900">{a.agent_ref}</span>
                  <span className="text-caption text-ink-500">v{a.version}</span>
                </div>
                <p className="text-body-sm text-ink-700">{a.definition.role}</p>
              </div>
              <Badge variant={a.status === 1 ? 'default' : 'secondary'}>{a.status === 1 ? '已启用' : '已停用'}</Badge>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setCopyFrom(definitionToFormState(a.definition, true))
                  setMode('create')
                }}
              >
                从此复制
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
