import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { DependencyErrorPanel } from '@/components/marketplace/DependencyErrorPanel'
import { apiClient, unwrap, ApiError, type FieldError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingResourceType = components['schemas']['ListingResourceType']

const TYPES: { value: ListingResourceType; label: string }[] = [
  { value: 'bundle', label: 'Bundle' },
  { value: 'agent', label: 'Agent' },
  { value: 'skill', label: 'Skill' },
  { value: 'mcp', label: 'MCP Server' },
]

/** 从依赖引导链接（DependencyErrorPanel）带过来的预填值。 */
export interface PublishFormInitial {
  type: ListingResourceType
  ref: string
  version?: string
}

export function PublishForm({
  onPublished,
  initial,
}: {
  onPublished: () => void
  initial?: PublishFormInitial
}) {
  const [resourceType, setResourceType] = useState<ListingResourceType>(initial?.type ?? 'bundle')
  const [resourceRef, setResourceRef] = useState(initial?.ref ?? '')
  const [version, setVersion] = useState(initial?.version ?? '')
  const [displayName, setDisplayName] = useState('')
  const [description, setDescription] = useState('')
  const [usage, setUsage] = useState('')
  const [outputs, setOutputs] = useState('')
  const [changelog, setChangelog] = useState('')
  const [pending, setPending] = useState(false)
  const [depErrors, setDepErrors] = useState<FieldError[] | null>(null)
  const [genericError, setGenericError] = useState<string | null>(null)

  // Pulls candidate refs/versions from the owner's own Bundle/Agent
  // catalogs so the form doesn't require memorizing exact version
  // strings — the DELETE-check equivalent (recursive dependency
  // closure) still happens server-side on submit either way.
  const candidates = useQuery({
    queryKey: ['publish-candidates', resourceType],
    enabled: resourceType === 'bundle' || resourceType === 'agent',
    queryFn: async () => {
      if (resourceType === 'bundle') {
        const res = unwrap<{ items: { bundle_ref: string; version: string }[] }>(
          await apiClient.GET('/bundles', {}),
        )
        return res.items
      }
      const res = unwrap<{ items: { agent_ref: string; version: string }[] }>(await apiClient.GET('/agents', {}))
      return res.items.map((a) => ({ bundle_ref: a.agent_ref, version: a.version }))
    },
  })

  async function submit() {
    setPending(true)
    setDepErrors(null)
    setGenericError(null)
    try {
      unwrap(
        await apiClient.POST('/marketplace/listings', {
          body: {
            resource_type: resourceType,
            resource_ref: resourceRef,
            version,
            display_meta: {
              display_name: displayName,
              description,
              usage: usage || undefined,
              io_description: outputs ? { outputs: outputs.split(',').map((s) => s.trim()).filter(Boolean) } : undefined,
            },
            changelog: changelog || undefined,
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      onPublished()
    } catch (err) {
      if (err instanceof ApiError && err.code === 70001 && err.details) {
        setDepErrors(err.details)
      } else {
        setGenericError(err instanceof ApiError ? err.message : '发布失败，请稍后重试')
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex w-full max-w-[720px] flex-col gap-space-6">
      {initial && (
        <p className="text-body-sm rounded-sm bg-blueprint-tint px-space-4 py-space-3 text-blueprint">
          从依赖引导跳转过来，已经帮你填好了 {initial.type}/{initial.ref}
          {initial.version ? `@${initial.version}` : ''}——发布完这一个，回到刚才那个页面重新校验即可。
        </p>
      )}
      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-type" className="text-label-md text-ink-700">
          资源类型
        </label>
        <Select value={resourceType} onValueChange={(v) => { setResourceType(v as ListingResourceType); setResourceRef(''); setVersion('') }}>
          <SelectTrigger id="publish-type" className="h-12 w-full rounded-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TYPES.map((t) => (
              <SelectItem key={t.value} value={t.value}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {(resourceType === 'bundle' || resourceType === 'agent') && candidates.data && candidates.data.length > 0 && (
        <div className="flex flex-col gap-space-2">
          <label htmlFor="publish-candidate" className="text-label-md text-ink-700">
            选择要发布的{resourceType === 'bundle' ? ' Bundle' : ' Agent'}
          </label>
          <Select
            value={resourceRef ? `${resourceRef}@${version}` : undefined}
            onValueChange={(v) => {
              const [ref, ver] = v.split('@')
              setResourceRef(ref)
              setVersion(ver)
            }}
          >
            <SelectTrigger id="publish-candidate" className="h-12 w-full rounded-sm">
              <SelectValue placeholder="选择…" />
            </SelectTrigger>
            <SelectContent>
              {candidates.data.map((c) => (
                <SelectItem key={`${c.bundle_ref}@${c.version}`} value={`${c.bundle_ref}@${c.version}`}>
                  {c.bundle_ref} v{c.version}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="grid grid-cols-2 gap-space-4">
        <div className="flex flex-col gap-space-2">
          <label htmlFor="publish-ref" className="text-label-md text-ink-700">
            resource_ref
          </label>
          <Input id="publish-ref" value={resourceRef} onChange={(e) => setResourceRef(e.target.value)} className="h-12 rounded-sm" />
        </div>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="publish-version" className="text-label-md text-ink-700">
            version
          </label>
          <Input id="publish-version" value={version} onChange={(e) => setVersion(e.target.value)} className="h-12 rounded-sm" placeholder="1.0" />
        </div>
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-display-name" className="text-label-md text-ink-700">
          展示名称
        </label>
        <Input id="publish-display-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} className="h-12 rounded-sm" placeholder="全栈应用生成器" />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-description" className="text-label-md text-ink-700">
          简介
        </label>
        <Textarea id="publish-description" value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-usage" className="text-label-md text-ink-700">
          使用说明（可选）
        </label>
        <Textarea id="publish-usage" value={usage} onChange={(e) => setUsage(e.target.value)} rows={3} />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-outputs" className="text-label-md text-ink-700">
          输出字段（逗号分隔，可选）
        </label>
        <Input id="publish-outputs" value={outputs} onChange={(e) => setOutputs(e.target.value)} className="h-12 rounded-sm" placeholder="architecture_doc, code_diff" />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="publish-changelog" className="text-label-md text-ink-700">
          更新说明（可选）
        </label>
        <Textarea id="publish-changelog" value={changelog} onChange={(e) => setChangelog(e.target.value)} rows={2} />
      </div>

      {depErrors && <DependencyErrorPanel errors={depErrors} onRevalidate={submit} />}
      {genericError && (
        <p role="alert" className="text-body-sm text-rust">
          {genericError}
        </p>
      )}

      <Button size="lg" disabled={pending || !resourceRef || !version || !displayName || !description} onClick={submit} className="self-start">
        {pending ? '发布中…' : '发布'}
      </Button>
    </div>
  )
}
