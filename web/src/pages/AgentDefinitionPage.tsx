import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { EmptyRail } from '@/components/common/Rail'
import { Ref, Section } from '@/components/common/Page'
import { cn } from '@/lib/utils'
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
        <div className="flex items-center justify-between border-b border-border pb-space-3">
          <h2 className="text-display-md text-ink-900">{copyFrom ? '复制智能体' : '新建智能体'}</h2>
          <Button
            variant="outline"
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
    <Section
      title="我的智能体"
      aside={
        <Button size="sm" onClick={() => setMode('create')}>
          新建智能体
        </Button>
      }
    >
      {query.isLoading && <ListSkeleton />}
      {query.isError && <ErrorPanel message="智能体列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyRail
          title="先定义一个角色"
          description="一个智能体（Agent）就是一个角色：它是谁、能用哪些资源、单轮最多花多少 token。应用编排的就是这些角色。"
          action={
            <Button size="sm" onClick={() => setMode('create')}>
              新建智能体
            </Button>
          }
        />
      )}

      {items.length > 0 && (
        <ul className="overflow-hidden rounded-lg border border-border bg-surface">
          {items.map((a) => (
            <li
              key={a.id}
              className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0"
            >
              <span
                aria-hidden
                className={cn(
                  'size-2 shrink-0 rounded-full',
                  a.status === 1 ? 'bg-moss' : 'bg-border-strong',
                )}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-center gap-space-2">
                  <Ref>{a.agent_ref}</Ref>
                  <span className="text-caption tabular text-ink-500">v{a.version}</span>
                </span>
                <span className="text-body-sm truncate text-ink-700">{a.definition.role}</span>
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setCopyFrom(definitionToFormState(a.definition, true))
                  setMode('create')
                }}
              >
                以此为模板
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}
