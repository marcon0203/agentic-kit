import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { GripVertical, Plus } from 'lucide-react'

import { Input } from '@/components/ui/input'
import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type Agent = components['schemas']['Agent']

export const AGENT_DRAG_MIME = 'application/x-agentic-kit-agent'

/** Left panel — spec-17's draggable Agent list. Dragging an item onto the
 * canvas creates a node (handled by BundleEditorPage's onDrop); clicking it
 * does the same thing at the canvas centre, because drag-and-drop as the
 * *only* way in is both slower for a quick build and unreachable by
 * keyboard. */
export function AgentPanel({ onAdd }: { onAdd: (agent: { ref: string; version: string }) => void }) {
  const [search, setSearch] = useState('')
  const query = useQuery({
    queryKey: ['agents'],
    queryFn: async () => unwrap<{ items: Agent[] }>(await apiClient.GET('/agents', {})),
  })

  const items = (query.data?.items ?? []).filter((a) => a.agent_ref.includes(search) || a.definition.role.includes(search))

  return (
    <div className="flex h-full flex-col gap-space-3 overflow-hidden border-r border-border bg-surface p-space-4">
      <div className="flex flex-col gap-space-2">
        <p className="text-label-md text-ink-900">智能体</p>
        <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索 Agent" className="h-9" />
        <p className="text-caption text-ink-500">拖到画布，或点一下加到画布中央</p>
      </div>

      {query.isLoading && <p className="text-body-sm text-ink-500">加载中…</p>}
      {query.isSuccess && items.length === 0 && (
        <p className="text-body-sm text-ink-500">
          {search ? '没有匹配的智能体' : '还没有智能体，先在智能体管理页创建一个'}
        </p>
      )}

      <ul className="flex flex-col gap-space-2 overflow-y-auto">
        {items.map((a) => (
          <li key={a.id}>
            <button
              type="button"
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData(AGENT_DRAG_MIME, JSON.stringify({ ref: a.agent_ref, version: a.version }))
                e.dataTransfer.effectAllowed = 'move'
              }}
              onClick={() => onAdd({ ref: a.agent_ref, version: a.version })}
              title={`把 ${a.agent_ref} 加到画布`}
              className="group flex w-full cursor-grab select-none items-center gap-space-2 rounded-sm border border-border-strong bg-surface-page px-space-3 py-space-2 text-left transition-colors hover:border-blueprint hover:bg-surface-muted active:cursor-grabbing"
            >
              <GripVertical className="size-3.5 shrink-0 text-ink-500" aria-hidden />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-ref truncate text-body-sm font-medium text-ink-900">{a.agent_ref}</span>
                <span className="text-caption truncate text-ink-500">{a.definition.role}</span>
              </span>
              <Plus className="size-3.5 shrink-0 text-ink-500 opacity-0 transition-opacity group-hover:opacity-100" aria-hidden />
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
