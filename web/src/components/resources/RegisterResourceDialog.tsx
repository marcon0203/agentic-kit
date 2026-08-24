import { useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ResourceType = components['schemas']['ResourceType']

const TYPE_LABEL: Record<ResourceType, string> = {
  tool: 'Tool',
  skill: 'Skill',
  mcp: 'MCP Server',
  knowledge_base: '知识库',
}

export function RegisterResourceDialog({
  type,
  open,
  onOpenChange,
  onCreated,
}: {
  type: ResourceType
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}) {
  const [ref, setRef] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [configText, setConfigText] = useState('{}')
  const [refError, setRefError] = useState<string | null>(null)
  const [configError, setConfigError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const refPattern = /^[a-z][a-z0-9_-]*$/

  function reset() {
    setRef('')
    setDisplayName('')
    setConfigText('{}')
    setRefError(null)
    setConfigError(null)
    setSubmitError(null)
  }

  function validateRef(value: string): boolean {
    if (!refPattern.test(value)) {
      setRefError('必须以小写字母开头，只能包含小写字母、数字、- 和 _')
      return false
    }
    setRefError(null)
    return true
  }

  function validateConfig(value: string): Record<string, unknown> | null {
    try {
      const parsed = JSON.parse(value)
      setConfigError(null)
      return parsed
    } catch {
      setConfigError('必须是合法的 JSON 对象')
      return null
    }
  }

  async function submit() {
    const refOk = validateRef(ref)
    const config = validateConfig(configText)
    if (!refOk || config === null) return

    setPending(true)
    setSubmitError(null)
    try {
      unwrap(
        await apiClient.POST('/resources', {
          body: { type, ref, display_name: displayName || undefined, config },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      reset()
      onOpenChange(false)
      onCreated()
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : '注册失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) reset()
        onOpenChange(v)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>注册 {TYPE_LABEL[type]}</DialogTitle>
          <DialogDescription>config 中含 api_key/token/secret 等字段的值会被加密存储，不会在列表中回显。</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-space-4">
          <div className="flex flex-col gap-space-2">
            <label htmlFor="resource-ref" className="text-label-md text-ink-700">
              ref
            </label>
            <Input
              id="resource-ref"
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              onBlur={(e) => validateRef(e.target.value)}
              aria-invalid={!!refError}
              aria-describedby={refError ? 'resource-ref-error' : undefined}
              className={cn(refError && 'border-rust', !refError && ref && 'border-moss')}
              placeholder="internal-search"
            />
            {refError && (
              <p id="resource-ref-error" className="text-caption text-rust">
                {refError}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-space-2">
            <label htmlFor="resource-name" className="text-label-md text-ink-700">
              显示名称（可选）
            </label>
            <Input id="resource-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
          </div>

          <div className="flex flex-col gap-space-2">
            <label htmlFor="resource-config" className="text-label-md text-ink-700">
              config（JSON）
            </label>
            <Textarea
              id="resource-config"
              value={configText}
              onChange={(e) => setConfigText(e.target.value)}
              onBlur={(e) => validateConfig(e.target.value)}
              aria-invalid={!!configError}
              aria-describedby={configError ? 'resource-config-error' : undefined}
              className={cn('font-mono', configError && 'border-rust')}
              rows={6}
            />
            {configError && (
              <p id="resource-config-error" className="text-caption text-rust">
                {configError}
              </p>
            )}
          </div>

          {submitError && (
            <p role="alert" className="text-body-sm text-rust">
              {submitError}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending || !ref} onClick={submit}>
            {pending ? '注册中…' : '注册'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
