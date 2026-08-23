import { useState } from 'react'
import { cn } from '@/lib/utils'
import { StatusChip } from '@/components/run/StatusChip'
import type { NodeBubbleState } from '@/lib/runs/timeline'

const MAX_LINES = 12

/** ds-user-bubble — right-aligned, surface-muted. */
export function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-[72%] rounded-lg bg-surface-muted px-space-5 py-space-4 text-body-lg text-ink-900">
        {text}
      </div>
    </div>
  )
}

/** ds-agent-bubble — design-system.md's core Chat component. */
export function AgentBubble({ node, bubble, isBlackbox }: { node: string; bubble: NodeBubbleState; isBlackbox: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const borderColor =
    bubble.status === 'failed' ? 'border-l-[var(--color-error)]' : bubble.status === 'done' ? 'border-l-[var(--color-success)]' : 'border-l-primary'

  const lines = bubble.text.split('\n')
  const overflow = !isBlackbox && lines.length > MAX_LINES && !expanded
  const shownText = overflow ? lines.slice(0, MAX_LINES).join('\n') : bubble.text

  return (
    <div
      className={cn('flex-1 rounded-lg border border-border border-l-2 bg-surface p-space-5 shadow-sm', borderColor)}
      aria-busy={bubble.isBusy}
    >
      <div className="mb-space-2 flex items-center gap-space-2">
        <div
          aria-hidden
          className="flex size-8 shrink-0 items-center justify-center rounded-full bg-surface-muted text-body-sm font-medium text-ink-700"
        >
          {node.slice(0, 1).toUpperCase()}
        </div>
        <span className="text-label-md text-ink-900">{node}</span>
        {isBlackbox && (
          <span className="text-caption inline-flex items-center gap-1 rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700">
            黑盒
          </span>
        )}
        <StatusChip
          status={bubble.status === 'done' ? 'done' : bubble.status === 'failed' ? 'failed' : 'running'}
          className="ml-auto"
        />
      </div>

      {bubble.status === 'failed' ? (
        <p className="text-body-md text-[var(--color-error)]">{bubble.errorText ?? '节点执行失败'}</p>
      ) : (
        <>
          {!isBlackbox && bubble.toolCalls.length > 0 && (
            <ul className="mb-space-3 flex flex-col gap-space-1">
              {bubble.toolCalls.map((tc, i) => (
                <li
                  key={i}
                  className="text-body-sm flex items-center gap-space-2 rounded-xs bg-surface-muted px-space-3 py-space-2 text-ink-700"
                >
                  <span className="font-mono">{tc.name}</span>
                  <span className="text-caption ml-auto text-ink-500">
                    {tc.status === 'started' ? '调用中…' : '已完成'}
                  </span>
                </li>
              ))}
            </ul>
          )}

          <p className="text-body-lg whitespace-pre-wrap text-ink-900">
            {shownText}
            {bubble.isBusy && <span className="ml-0.5 inline-block w-[2px] animate-pulse bg-ink-900 align-middle">&nbsp;</span>}
          </p>
          {/* Only announce once finished — an aria-live region on the
              growing text would re-announce on every appended character. */}
          <span className="sr-only" aria-live="polite">
            {bubble.status === 'done' ? bubble.text : ''}
          </span>
          {overflow && (
            <button
              type="button"
              onClick={() => setExpanded(true)}
              className="text-body-sm mt-space-2 text-primary hover:underline"
            >
              展开全部
            </button>
          )}
          {bubble.status === 'done' && <p className="text-body-sm mt-space-3 text-primary">查看完整产出</p>}
        </>
      )}
    </div>
  )
}
