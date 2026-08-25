import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { ArrowLeft, Plus, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'

interface HeaderRow {
  key: string
  value: string
}

interface ProbeResult {
  ok: boolean
  tools?: { name: string; description?: string }[]
  error?: string
}

const refPattern = /^[a-z][a-z0-9_-]*$/

/**
 * 接入 MCP Server 的专属页面，取代通用的 ref+display_name+JSON 弹窗——
 * Header 列表要能逐行增删，"检测"要能把探测到的工具列表铺开展示，这些都
 * 挤不进一个 Dialog（spec-05a）。
 */
export function McpServerEditorPage() {
  const navigate = useNavigate()

  const [ref, setRef] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [description, setDescription] = useState('')
  const [url, setUrl] = useState('')
  const [headers, setHeaders] = useState<HeaderRow[]>([])
  const [refError, setRefError] = useState<string | null>(null)

  const [probing, setProbing] = useState(false)
  const [probeResult, setProbeResult] = useState<ProbeResult | null>(null)

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  function validateRef(value: string): boolean {
    if (!refPattern.test(value)) {
      setRefError('必须以小写字母开头，只能包含小写字母、数字、- 和 _')
      return false
    }
    setRefError(null)
    return true
  }

  function updateHeader(index: number, patch: Partial<HeaderRow>) {
    setHeaders((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)))
  }

  function addHeader() {
    setHeaders((rows) => [...rows, { key: '', value: '' }])
  }

  function removeHeader(index: number) {
    setHeaders((rows) => rows.filter((_, i) => i !== index))
  }

  function activeHeaders() {
    return headers.filter((h) => h.key.trim() !== '')
  }

  async function probe() {
    if (!url) return
    setProbing(true)
    setProbeResult(null)
    try {
      const res = await apiClient.POST('/resources/mcp/probe', {
        body: { url, headers: activeHeaders() },
      })
      setProbeResult(unwrap<ProbeResult>(res))
    } catch (err) {
      setProbeResult({ ok: false, error: err instanceof ApiError ? err.message : '探测失败，请稍后重试' })
    } finally {
      setProbing(false)
    }
  }

  async function save() {
    if (!validateRef(ref)) return
    setSaving(true)
    setSaveError(null)
    try {
      unwrap(
        await apiClient.POST('/resources', {
          body: {
            type: 'mcp',
            ref,
            display_name: displayName || undefined,
            config: {
              endpoint: url,
              description: description || undefined,
              headers: activeHeaders(),
            },
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      toast.success('已保存')
      navigate('/apps/mcp')
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-space-6 py-space-4">
      <div className="flex items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/mcp')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span className="text-body-sm text-ink-500">接入 MCP Server</span>
      </div>

      <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-6">
        <div className="flex flex-col gap-space-2">
          <label htmlFor="mcp-ref" className="text-label-md text-ink-700">
            ref
          </label>
          <Input
            id="mcp-ref"
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            onBlur={(e) => validateRef(e.target.value)}
            aria-invalid={!!refError}
            className={cn(refError && 'border-rust', !refError && ref && 'border-moss')}
            placeholder="internal-mcp"
          />
          {refError && <p className="text-caption text-rust">{refError}</p>}
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="mcp-name" className="text-label-md text-ink-700">
            显示名称（可选）
          </label>
          <Input id="mcp-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="mcp-description" className="text-label-md text-ink-700">
            说明（可选）
          </label>
          <Textarea id="mcp-description" value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="mcp-url" className="text-label-md text-ink-700">
            URL
          </label>
          <Input
            id="mcp-url"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value)
              setProbeResult(null)
            }}
            placeholder="https://mcp.example.com"
          />
        </div>

        <div className="flex flex-col gap-space-2">
          <div className="flex items-center justify-between">
            <span className="text-label-md text-ink-700">Header</span>
            <Button type="button" variant="outline" size="sm" onClick={addHeader}>
              <Plus className="size-3.5" aria-hidden />
              添加 Header
            </Button>
          </div>
          {headers.length === 0 && <p className="text-body-sm text-ink-500">还没有自定义 Header，比如 Authorization。</p>}
          {headers.map((row, i) => (
            <div key={i} className="flex items-center gap-space-2">
              <Input
                value={row.key}
                onChange={(e) => updateHeader(i, { key: e.target.value })}
                placeholder="Authorization"
                className="flex-1"
              />
              <Input
                value={row.value}
                onChange={(e) => updateHeader(i, { value: e.target.value })}
                placeholder="Bearer sk-..."
                type="password"
                className="flex-1"
              />
              <Button type="button" variant="ghost" size="sm" onClick={() => removeHeader(i)} aria-label="删除该 Header">
                <Trash2 className="size-4" aria-hidden />
              </Button>
            </div>
          ))}
        </div>

        <div className="flex flex-col gap-space-2">
          <Button type="button" variant="outline" disabled={!url || probing} onClick={probe} className="self-start">
            {probing ? '检测中…' : '检测'}
          </Button>

          {probeResult?.ok && (
            <div className="flex flex-col gap-space-2 rounded-md border border-moss bg-moss-tint px-space-4 py-space-3">
              <p className="text-body-sm text-moss">
                检测通过，发现 {probeResult.tools?.length ?? 0} 个工具
              </p>
              {probeResult.tools && probeResult.tools.length > 0 && (
                <ul className="flex flex-col gap-space-1">
                  {probeResult.tools.map((t) => (
                    <li key={t.name} className="text-body-sm text-ink-700">
                      <span className="text-ref">{t.name}</span>
                      {t.description && <span className="text-ink-500"> — {t.description}</span>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
          {probeResult && !probeResult.ok && (
            <p role="alert" className="text-body-sm rounded-md border border-rust bg-rust-tint px-space-4 py-space-3 text-rust">
              检测未通过：{probeResult.error}
            </p>
          )}
        </div>

        {saveError && (
          <p role="alert" className="text-body-sm text-rust">
            {saveError}
          </p>
        )}
      </div>

      <div className="flex items-center gap-space-3 self-end">
        <Button variant="outline" onClick={() => navigate('/apps/mcp')} disabled={saving}>
          取消
        </Button>
        <Button disabled={saving || !ref || !url} onClick={save} className="bg-gradient-cta text-white hover:opacity-90">
          {saving ? '保存中…' : '保存'}
        </Button>
      </div>
    </div>
  )
}
