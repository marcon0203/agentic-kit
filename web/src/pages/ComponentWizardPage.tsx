import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { ArrowLeft, Blocks, Boxes, Puzzle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { COMPONENT_CATEGORIES, type ComponentCategory } from '@/lib/components/taxonomy'

type Step = 'type' | 'tool-shape' | 'tool-http' | 'tool-openapi' | 'sandbox'
type ComponentType = 'tool' | 'sandbox'

interface OpenAPIOperation {
  operation_id: string
  method: string
  path: string
  summary?: string
}

const refPattern = /^[a-z][a-z0-9_-]*$/

/**
 * 组件的两步新建向导（spec-05a §4）——Step 1 选组件类型（Tool/沙箱环境已
 * 实现可选，插件预留禁用），Step 2 按类型给出对应表单：Tool 再分 http 单
 * 接口 / OpenAPI 批量导入两种形态，沙箱环境直接是一张 Daytona 配置表单。
 */
export function ComponentWizardPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('type')

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-space-6 py-space-4">
      <div className="flex items-center gap-space-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            if (step === 'type') navigate('/apps/tool')
            else if (step === 'tool-http' || step === 'tool-openapi') setStep('tool-shape')
            else setStep('type')
          }}
        >
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span className="text-body-sm text-ink-500">新建组件</span>
      </div>

      {step === 'type' && <TypeStep onPick={(t) => setStep(t === 'tool' ? 'tool-shape' : 'sandbox')} />}
      {step === 'tool-shape' && (
        <ToolShapeStep onPick={(shape) => setStep(shape === 'http' ? 'tool-http' : 'tool-openapi')} />
      )}
      {step === 'tool-http' && <ToolHTTPForm />}
      {step === 'tool-openapi' && <ToolOpenAPIImport />}
      {step === 'sandbox' && <SandboxForm />}
    </div>
  )
}

function TypeStep({ onPick }: { onPick: (t: ComponentType) => void }) {
  return (
    <div className="grid grid-cols-1 gap-space-4 sm:grid-cols-3">
      <TypeCard
        icon={Blocks}
        title="Tool"
        description="调用一个外部接口——手填单个 endpoint，或从 OpenAPI spec 批量导入一批 operation。"
        onClick={() => onPick('tool')}
      />
      <TypeCard
        icon={Boxes}
        title="沙箱环境"
        description="接入一个 Daytona 沙箱，让 Agent 能执行代码、跑 shell 命令。"
        onClick={() => onPick('sandbox')}
      />
      <TypeCard icon={Puzzle} title="插件" description="即将支持。" disabled />
    </div>
  )
}

function TypeCard({
  icon: Icon,
  title,
  description,
  onClick,
  disabled,
}: {
  icon: typeof Blocks
  title: string
  description: string
  onClick?: () => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex flex-col items-start gap-space-2 rounded-lg border border-border bg-surface p-space-5 text-left transition-colors',
        disabled ? 'cursor-not-allowed opacity-50' : 'hover:border-border-strong',
      )}
    >
      <Icon className="size-5 text-ink-500" aria-hidden />
      <span className="text-label-md text-ink-900">{title}</span>
      <span className="text-body-sm text-ink-500">{description}</span>
      {disabled && <span className="text-caption text-ink-500">即将支持</span>}
    </button>
  )
}

/**
 * 使用场景——只影响组件广场的筛选和卡片标签，运行时不读它，所以是可选的，
 * 留空就落到"未分类"。
 */
function CategorySelect({
  value,
  onChange,
}: {
  value: ComponentCategory | ''
  onChange: (value: ComponentCategory | '') => void
}) {
  return (
    <div className="flex flex-col gap-space-2">
      <span className="text-label-md text-ink-700">使用场景（可选）</span>
      <Select
        value={value || 'none'}
        onValueChange={(v) => onChange(v === 'none' ? '' : (v as ComponentCategory))}
      >
        <SelectTrigger className="w-full" aria-label="使用场景">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="none">未分类</SelectItem>
          {COMPONENT_CATEGORIES.map((c) => (
            <SelectItem key={c.value} value={c.value}>
              {c.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-caption text-ink-500">只用于组件广场的筛选和分类标签，不影响 Agent 怎么调用它。</p>
    </div>
  )
}

function ToolShapeStep({ onPick }: { onPick: (shape: 'http' | 'openapi') => void }) {
  return (
    <div className="grid grid-cols-1 gap-space-4 sm:grid-cols-2">
      <TypeCard
        icon={Blocks}
        title="HTTP 单接口"
        description="手填一个 endpoint，Agent 调用时把输入原样 POST 过去。"
        onClick={() => onPick('http')}
      />
      <TypeCard
        icon={Boxes}
        title="OpenAPI 导入"
        description="贴 spec 的 URL 或内容，预览并勾选要开放的 operation，批量注册。"
        onClick={() => onPick('openapi')}
      />
    </div>
  )
}

function ToolHTTPForm() {
  const navigate = useNavigate()
  const [ref, setRef] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState<ComponentCategory | ''>('')
  const [refError, setRefError] = useState<string | null>(null)
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

  async function save() {
    if (!validateRef(ref)) return
    setSaving(true)
    setSaveError(null)
    try {
      unwrap(
        await apiClient.POST('/resources', {
          body: {
            type: 'tool',
            ref,
            display_name: displayName || undefined,
            config: { endpoint, description: description || undefined, category: category || undefined },
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      toast.success('已保存')
      navigate('/apps/tool')
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-6">
      <div className="flex flex-col gap-space-2">
        <label htmlFor="tool-ref" className="text-label-md text-ink-700">
          ref
        </label>
        <Input
          id="tool-ref"
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          onBlur={(e) => validateRef(e.target.value)}
          aria-invalid={!!refError}
          className={cn(refError && 'border-rust', !refError && ref && 'border-moss')}
          placeholder="internal-search"
        />
        {refError && <p className="text-caption text-rust">{refError}</p>}
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="tool-name" className="text-label-md text-ink-700">
          显示名称（可选）
        </label>
        <Input id="tool-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="tool-endpoint" className="text-label-md text-ink-700">
          endpoint
        </label>
        <Input
          id="tool-endpoint"
          value={endpoint}
          onChange={(e) => setEndpoint(e.target.value)}
          placeholder="https://api.example.com/search"
        />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="tool-description" className="text-label-md text-ink-700">
          说明（可选）
        </label>
        <Textarea id="tool-description" value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
      </div>

      <CategorySelect value={category} onChange={setCategory} />

      {saveError && (
        <p role="alert" className="text-body-sm text-rust">
          {saveError}
        </p>
      )}

      <Button
        disabled={saving || !ref || !endpoint}
        onClick={save}
        className="self-end bg-gradient-cta text-white hover:opacity-90"
      >
        {saving ? '保存中…' : '保存'}
      </Button>
    </div>
  )
}

function ToolOpenAPIImport() {
  const navigate = useNavigate()
  const [specURL, setSpecURL] = useState('')
  const [specContent, setSpecContent] = useState('')
  const [parsing, setParsing] = useState(false)
  const [parseError, setParseError] = useState<string | null>(null)
  const [operations, setOperations] = useState<OpenAPIOperation[] | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [baseRef, setBaseRef] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [category, setCategory] = useState<ComponentCategory | ''>('')
  const [baseRefError, setBaseRefError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  function validateBaseRef(value: string): boolean {
    if (!refPattern.test(value)) {
      setBaseRefError('必须以小写字母开头，只能包含小写字母、数字、- 和 _')
      return false
    }
    setBaseRefError(null)
    return true
  }

  async function parse() {
    if (!specURL && !specContent) return
    setParsing(true)
    setParseError(null)
    setOperations(null)
    try {
      const result = unwrap<{ base_url?: string; operations: OpenAPIOperation[] }>(
        await apiClient.POST('/resources/components/import-openapi', {
          body: { spec_url: specURL || undefined, spec_content: specContent || undefined },
        }),
      )
      setOperations(result.operations)
      setSelected(new Set(result.operations.map((op) => op.operation_id)))
      if (result.base_url) setBaseURL(result.base_url)
    } catch (err) {
      setParseError(err instanceof ApiError ? err.message : '解析失败，请检查 spec 是否合法')
    } finally {
      setParsing(false)
    }
  }

  function toggle(operationID: string, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (checked) next.add(operationID)
      else next.delete(operationID)
      return next
    })
  }

  async function create() {
    if (!operations || !validateBaseRef(baseRef) || !baseURL || selected.size === 0) return
    setCreating(true)
    setCreateError(null)
    try {
      unwrap(
        await apiClient.POST('/resources/components/batch', {
          body: {
            base_ref: baseRef,
            base_url: baseURL,
            category: category || undefined,
            operations: operations.filter((op) => selected.has(op.operation_id)),
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      toast.success(`已创建 ${selected.size} 个组件`)
      navigate('/apps/tool')
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : '创建失败，请稍后重试')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-6">
      <div className="flex flex-col gap-space-2">
        <label htmlFor="openapi-url" className="text-label-md text-ink-700">
          spec URL
        </label>
        <Input
          id="openapi-url"
          value={specURL}
          onChange={(e) => setSpecURL(e.target.value)}
          placeholder="https://api.example.com/openapi.json"
        />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="openapi-content" className="text-label-md text-ink-700">
          或直接贴 spec 内容（YAML / JSON，二选一）
        </label>
        <Textarea
          id="openapi-content"
          value={specContent}
          onChange={(e) => setSpecContent(e.target.value)}
          className="font-mono"
          rows={6}
        />
      </div>

      {parseError && (
        <p role="alert" className="text-body-sm text-rust">
          {parseError}
        </p>
      )}

      <Button
        type="button"
        variant="outline"
        disabled={parsing || (!specURL && !specContent)}
        onClick={parse}
        className="self-start"
      >
        {parsing ? '解析中…' : '解析'}
      </Button>

      {operations && (
        <>
          <div className="flex flex-col gap-space-2">
            <label htmlFor="openapi-base-ref" className="text-label-md text-ink-700">
              base_ref
            </label>
            <Input
              id="openapi-base-ref"
              value={baseRef}
              onChange={(e) => setBaseRef(e.target.value)}
              onBlur={(e) => validateBaseRef(e.target.value)}
              aria-invalid={!!baseRefError}
              className={cn(baseRefError && 'border-rust', !baseRefError && baseRef && 'border-moss')}
              placeholder="petstore"
            />
            {baseRefError && <p className="text-caption text-rust">{baseRefError}</p>}
            <p className="text-caption text-ink-500">每个 operation 会注册为 {baseRef || '{base_ref}'}__{'{operation_id}'}</p>
          </div>

          <div className="flex flex-col gap-space-2">
            <label htmlFor="openapi-base-url" className="text-label-md text-ink-700">
              base_url
            </label>
            <Input id="openapi-base-url" value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder="https://api.example.com/v1" />
          </div>

          <CategorySelect value={category} onChange={setCategory} />

          <div className="flex flex-col gap-space-2">
            <span className="text-label-md text-ink-700">勾选要开放给 Agent 的 operation（{selected.size}/{operations.length}）</span>
            {operations.length === 0 && <p className="text-body-sm text-ink-500">这份 spec 没有解析出任何 operation。</p>}
            <ul className="flex flex-col gap-space-2 rounded-md border border-border">
              {operations.map((op) => (
                <li key={op.operation_id} className="flex items-center gap-space-3 border-b border-border px-space-4 py-space-2 last:border-0">
                  <Checkbox
                    checked={selected.has(op.operation_id)}
                    onCheckedChange={(checked) => toggle(op.operation_id, checked === true)}
                  />
                  <span className="text-caption w-14 shrink-0 text-ink-500">{op.method}</span>
                  <span className="text-ref">{op.path}</span>
                  {op.summary && <span className="text-body-sm text-ink-500">{op.summary}</span>}
                </li>
              ))}
            </ul>
          </div>

          {createError && (
            <p role="alert" className="text-body-sm text-rust">
              {createError}
            </p>
          )}

          <Button
            disabled={creating || !baseRef || !baseURL || selected.size === 0}
            onClick={create}
            className="self-end bg-gradient-cta text-white hover:opacity-90"
          >
            {creating ? '创建中…' : `创建 ${selected.size} 个组件`}
          </Button>
        </>
      )}
    </div>
  )
}

function SandboxForm() {
  const navigate = useNavigate()
  const [ref, setRef] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [apiURL, setApiURL] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [organizationID, setOrganizationID] = useState('')
  const [category, setCategory] = useState<ComponentCategory | ''>('')
  const [refError, setRefError] = useState<string | null>(null)
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

  async function save() {
    if (!validateRef(ref)) return
    setSaving(true)
    setSaveError(null)
    try {
      unwrap(
        await apiClient.POST('/resources', {
          body: {
            type: 'tool',
            ref,
            display_name: displayName || undefined,
            config: {
              component_type: 'sandbox',
              api_url: apiURL,
              api_key: apiKey,
              organization_id: organizationID || undefined,
              category: category || undefined,
            },
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      toast.success('已保存')
      navigate('/apps/tool')
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-6">
      <p className="text-body-sm text-ink-500">
        接入一个 Daytona 沙箱——Agent 会拿到两个工具：执行代码（Python/JS/TS）和执行 shell 命令。沙箱按需懒建，闲置 15 分钟自动停止。
      </p>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="sandbox-ref" className="text-label-md text-ink-700">
          ref
        </label>
        <Input
          id="sandbox-ref"
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          onBlur={(e) => validateRef(e.target.value)}
          aria-invalid={!!refError}
          className={cn(refError && 'border-rust', !refError && ref && 'border-moss')}
          placeholder="dev-sandbox"
        />
        {refError && <p className="text-caption text-rust">{refError}</p>}
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="sandbox-name" className="text-label-md text-ink-700">
          显示名称（可选）
        </label>
        <Input id="sandbox-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="sandbox-api-url" className="text-label-md text-ink-700">
          Daytona API URL
        </label>
        <Input id="sandbox-api-url" value={apiURL} onChange={(e) => setApiURL(e.target.value)} placeholder="https://app.daytona.io/api" />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="sandbox-api-key" className="text-label-md text-ink-700">
          API Key
        </label>
        <Input id="sandbox-api-key" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
      </div>

      <div className="flex flex-col gap-space-2">
        <label htmlFor="sandbox-org" className="text-label-md text-ink-700">
          Organization ID（可选）
        </label>
        <Input id="sandbox-org" value={organizationID} onChange={(e) => setOrganizationID(e.target.value)} />
      </div>

      <CategorySelect value={category} onChange={setCategory} />

      {saveError && (
        <p role="alert" className="text-body-sm text-rust">
          {saveError}
        </p>
      )}

      <Button
        disabled={saving || !ref || !apiURL || !apiKey}
        onClick={save}
        className="self-end bg-gradient-cta text-white hover:opacity-90"
      >
        {saving ? '保存中…' : '保存'}
      </Button>
    </div>
  )
}
