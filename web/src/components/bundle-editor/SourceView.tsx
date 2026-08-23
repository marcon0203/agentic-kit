import { useState } from 'react'

import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

/**
 * DSL 源码 tab (spec-17). Plain JSON, monospace, no real syntax
 * highlighter (would need a code-editor dependency well beyond this
 * task's scope) — but a real, live JSON.parse validity check that
 * blocks switching back to the canvas while the text doesn't parse,
 * with the parser's own error surfaced below.
 */
export function SourceView({
  value,
  onChange,
  onValidChange,
}: {
  value: string
  onChange: (text: string) => void
  onValidChange: (valid: boolean) => void
}) {
  const [error, setError] = useState<string | null>(null)

  function handleChange(text: string) {
    onChange(text)
    try {
      JSON.parse(text)
      setError(null)
      onValidChange(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : '解析失败')
      onValidChange(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-space-2 p-space-4">
      <Textarea
        value={value}
        onChange={(e) => handleChange(e.target.value)}
        spellCheck={false}
        className={cn('h-full flex-1 resize-none font-mono text-body-sm', error && 'border-[var(--color-error)]')}
      />
      {error && (
        <p role="alert" className="text-body-sm rounded-md border border-[var(--color-error)] px-space-3 py-space-2 text-[var(--color-error)]">
          JSON 语法错误，无法切回画布：{error}
        </p>
      )}
    </div>
  )
}
