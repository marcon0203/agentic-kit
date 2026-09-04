import { Pencil, Copy, Rocket } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type Agent = components['schemas']['Agent']
type AgentDefinition = components['schemas']['AgentDefinition']

interface AgentCardProps {
  agent: Agent
  onEdit: (id: string) => void
  onCopy: (agent: Agent) => void
  onPublish: (agent: Agent) => void
}

export function AgentCard({ agent, onEdit, onCopy, onPublish }: AgentCardProps) {
  const definition = agent.definition as AgentDefinition
  const subtitle = definition.persona ?? `${definition.model?.provider ?? ''}/${definition.model?.name ?? ''}`

  return (
    <div className="group flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-5 transition-colors hover:border-border-strong">
      <div className="flex items-start justify-between gap-space-3">
        <div className="flex items-center gap-space-3">
          <span
            aria-hidden
            className={cn(
              'size-2 shrink-0 rounded-full',
              agent.status === 1 ? 'bg-moss' : 'bg-border-strong',
            )}
          />
          <div className="flex flex-col">
            <span className="flex items-center gap-space-2">
              <Ref>{agent.agent_ref}</Ref>
              <span className="text-caption tabular text-ink-500">v{agent.version}</span>
            </span>
            <span className="text-body-sm text-ink-700">{definition.role}</span>
          </div>
        </div>
      </div>

      <p className="text-body-sm line-clamp-2 min-h-[2.5rem] text-ink-500">
        {typeof subtitle === 'string' ? subtitle : '—'}
      </p>

      <div className="mt-auto flex items-center justify-end gap-space-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => onPublish(agent)}
        >
          <Rocket className="mr-1 size-3.5" aria-hidden />
          发布
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onCopy(agent)}
        >
          <Copy className="mr-1 size-3.5" aria-hidden />
          以此为模板
        </Button>
        <Button
          size="sm"
          onClick={() => onEdit(agent.id)}
        >
          <Pencil className="mr-1 size-3.5" aria-hidden />
          编辑
        </Button>
      </div>
    </div>
  )
}
