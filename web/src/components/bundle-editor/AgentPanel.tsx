import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Input } from '@/components/ui/input'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Agent = components['schemas']['Agent']

export const AGENT_DRAG_MIME = 'application/x-agentic-kit-agent'

/** Left panel — spec-17's draggable Agent list. Dragging an item onto the
 * canvas creates a node (handled by BundleEditorPage's onDrop). */
export function AgentPanel() {
  const [search, setSearch] = useState('')
  const query = useQuery({
    queryKey: ['agents'],
    queryFn: async () => unwrap<{ items: Agent[] }>(await apiClient.GET('/agents', {})),
  })

  const items = (query.data?.items ?? []).filter((a) => a.agent_ref.includes(search) || a.definition.role.includes(search))

  return (
    <div className="flex h-full flex-col gap-space-4 border-r border-border bg-surface p-space-4">
      <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索 Agent" className="h-9" />

      {query.isLoading && <p className="text-body-sm text-ink-500">加载中…</p>}
      {query.isSuccess && items.length === 0 && (
        <p className="text-body-sm text-ink-500">
          {search ? '没有匹配的 Agent' : '还没有 Agent，先在 Agent 定义页创建一个'}
        </p>
      )}

      <ul className="flex flex-col gap-space-2 overflow-y-auto">
        {items.map((a) => (
          <li
            key={a.id}
            draggable
            onDragStart={(e) => {
              e.dataTransfer.setData(AGENT_DRAG_MIME, JSON.stringify({ ref: a.agent_ref, version: a.version }))
              e.dataTransfer.effectAllowed = 'move'
            }}
            className="text-ref cursor-grab select-none rounded-sm border border-border-strong bg-surface-page px-space-3 py-space-2 active:cursor-grabbing"
          >
            <p className="text-body-sm font-medium text-ink-900">{a.agent_ref}</p>
            <p className="text-caption text-ink-500">{a.definition.role}</p>
          </li>
        ))}
      </ul>
    </div>
  )
}
