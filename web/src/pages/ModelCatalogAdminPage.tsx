import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Panel, Ref, Section } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import { Can, useHasPermission } from '@/lib/rbac/usePermissions'
import type { components } from '@/lib/api/schema'

type CatalogProvider = components['schemas']['CatalogProvider']
type CatalogModel = components['schemas']['CatalogModel']
type CatalogModality = components['schemas']['CatalogModality']

const MODALITY_LABEL: Record<CatalogModality, string> = {
  text: '文本',
  image: '图片',
  video: '视频',
  vision: '图文理解',
  embedding: '向量',
}

const MAX_ICON_BYTES = 200 * 1024

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

/**
 * 系统配置 → 模型提供商：先登记 Provider（名称 + 图标），再在它下面逐个添加
 * 模型（如 deepseek-v3），标注类型。这里管理的是"目录"本身——启用的条目会
 * 出现在模型广场（GET /model-catalog），和 /models 页面里"新增模型"接入的
 * 个人凭证是两件事：那边验证的是能不能真的调用，这里维护的是广场展示什么。
 */
export function ModelCatalogAdminPage() {
  const canView = useHasPermission('model_catalog.provider.view')
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [credentialTarget, setCredentialTarget] = useState<CatalogProvider | null>(null)

  const providersQuery = useQuery({
    queryKey: ['catalog-providers'],
    queryFn: async () => unwrap<CatalogProvider[]>(await apiClient.GET('/model-catalog/providers', {})),
    enabled: canView,
  })

  async function toggleProviderStatus(p: CatalogProvider) {
    setActionError(null)
    try {
      unwrap(
        await apiClient.PATCH('/model-catalog/providers/{id}', {
          params: { path: { id: p.id } },
          body: { status: p.status === 1 ? 2 : 1 },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['catalog-providers'] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  async function deleteProvider(p: CatalogProvider) {
    setActionError(null)
    try {
      unwrap(await apiClient.DELETE('/model-catalog/providers/{id}', { params: { path: { id: p.id } } }))
      queryClient.invalidateQueries({ queryKey: ['catalog-providers'] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '删除没能完成，请再试一次')
    }
  }

  if (!canView) {
    return (
      <EmptyRail
        title="没有查看权限"
        description="模型提供商是系统级配置，需要 model_catalog.provider.view 权限——找管理员在角色权限页面分配对应角色。"
      />
    )
  }

  const providers = providersQuery.data ?? []

  return (
    <div className="flex flex-col gap-space-6">
      <Section
        title="Provider 列表"
        aside={
          <Can permission="model_catalog.provider.create">
            <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
              新增 Provider
            </Button>
          </Can>
        }
      >
        {actionError && (
          <p role="alert" className="text-body-sm text-rust">
            {actionError}
          </p>
        )}

        {providersQuery.isLoading && <ListSkeleton />}
        {providersQuery.isError && (
          <ErrorPanel message="Provider 列表没能加载出来" onRetry={() => providersQuery.refetch()} />
        )}

        {providersQuery.isSuccess && providers.length === 0 && (
          <EmptyRail
            title="还没有登记任何 Provider"
            description="先新增一个 Provider（如 DeepSeek），再在它下面添加具体模型（如 deepseek-v3）。"
            action={
              <Can permission="model_catalog.provider.create">
                <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
                  新增 Provider
                </Button>
              </Can>
            }
          />
        )}

        {providers.length > 0 && (
          <ul className="flex flex-col gap-space-3">
            {providers.map((p) => (
              <li key={p.id}>
                <Panel padded={false}>
                  <div className="flex items-center gap-space-4 px-space-5 py-space-3">
                    <button
                      type="button"
                      onClick={() => setExpanded((cur) => (cur === p.id ? null : p.id))}
                      className="flex shrink-0 items-center text-ink-500 hover:text-ink-900"
                      aria-label={expanded === p.id ? '收起' : '展开管理模型'}
                    >
                      {expanded === p.id ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                    </button>
                    {p.icon ? (
                      <img src={p.icon} alt="" className="size-8 shrink-0 rounded-sm object-cover" />
                    ) : (
                      <span
                        aria-hidden
                        className="text-caption flex size-8 shrink-0 items-center justify-center rounded-sm bg-surface-muted font-medium text-ink-500"
                      >
                        {p.display_name.slice(0, 1).toUpperCase()}
                      </span>
                    )}
                    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="flex items-center gap-space-2">
                        <span className="text-body-md text-ink-900">{p.display_name}</span>
                        <Ref tone="muted">{p.key}</Ref>
                      </span>
                      {p.base_url && <span className="text-caption truncate text-ink-500">{p.base_url}</span>}
                    </span>
                    <span
                      className={cn('text-caption w-12 shrink-0 text-right', p.status === 1 ? 'text-moss' : 'text-ink-500')}
                    >
                      {p.status === 1 ? '已启用' : '已停用'}
                    </span>
                    <span className={cn('text-caption w-16 shrink-0 text-right', p.has_credential ? 'text-moss' : 'text-ink-500')}>
                      {p.has_credential ? '已配置凭证' : '未配置凭证'}
                    </span>
                    <Can permission="model_catalog.provider.create">
                      <Button variant="outline" size="sm" onClick={() => setCredentialTarget(p)}>
                        配置凭证
                      </Button>
                    </Can>
                    <Can permission="model_catalog.provider.toggle">
                      <Button variant="outline" size="sm" onClick={() => toggleProviderStatus(p)}>
                        {p.status === 1 ? '停用' : '启用'}
                      </Button>
                    </Can>
                    <Can permission="model_catalog.provider.delete">
                      <Button variant="ghost" size="sm" onClick={() => deleteProvider(p)}>
                        删除
                      </Button>
                    </Can>
                  </div>
                  {expanded === p.id && (
                    <div className="border-t border-border px-space-5 py-space-4">
                      <ProviderModels providerId={p.id} onError={setActionError} />
                    </div>
                  )}
                </Panel>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <CreateProviderDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={() => {
          queryClient.invalidateQueries({ queryKey: ['catalog-providers'] })
          setCreateOpen(false)
        }}
      />

      <ProviderCredentialDialog
        provider={credentialTarget}
        onOpenChange={(v) => {
          if (!v) setCredentialTarget(null)
        }}
        onSaved={() => {
          queryClient.invalidateQueries({ queryKey: ['catalog-providers'] })
          setCredentialTarget(null)
        }}
      />
    </div>
  )
}

/**
 * 管理员统一凭证：一个 provider 的组织级默认 api_key + base_url，供没有在
 * /models（模型广场）自己接入个人凭证的用户兜底使用——两套凭证并存，个人
 * 凭证优先。api_key 从不回显明文，留空提交表示不修改已保存的密钥。
 */
function ProviderCredentialDialog({
  provider,
  onOpenChange,
  onSaved,
}: {
  provider: CatalogProvider | null
  onOpenChange: (v: boolean) => void
  onSaved: () => void
}) {
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    setApiKey('')
    setBaseUrl(provider?.base_url ?? '')
    setError(null)
  }, [provider])

  async function submit() {
    if (!provider) return
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.PUT('/model-catalog/providers/{id}/credential', {
          params: { path: { id: provider.id } },
          body: { api_key: apiKey || undefined, base_url: baseUrl },
        }),
      )
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={provider !== null} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>配置管理员统一凭证{provider ? ` · ${provider.display_name}` : ''}</DialogTitle>
          <DialogDescription>
            组织级默认凭证：用户在模型广场（/models）没有为这个 Provider 接入个人凭证时，运行时会用这里登记的
            api_key 兜底调用。和用户各自的个人凭证是两回事，个人凭证优先。
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="credential-api-key" className="text-label-md text-ink-700">
            API Key {provider?.has_credential && <span className="text-caption text-ink-500">（已设置，留空则不修改）</span>}
          </label>
          <Input
            id="credential-api-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={provider?.has_credential ? '••••••••' : 'sk-...'}
            className="h-12 rounded-sm"
          />

          <label htmlFor="credential-base-url" className="text-label-md text-ink-700">
            Base URL
          </label>
          <Input
            id="credential-base-url"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://api.example.com/v1"
            className="h-12 rounded-sm"
          />

          {error && (
            <p role="alert" className="text-caption text-rust">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ProviderModels({ providerId, onError }: { providerId: string; onError: (msg: string | null) => void }) {
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)

  const modelsQuery = useQuery({
    queryKey: ['catalog-models', providerId],
    queryFn: async () =>
      unwrap<CatalogModel[]>(
        await apiClient.GET('/model-catalog/providers/{id}/models', { params: { path: { id: providerId } } }),
      ),
  })

  async function toggleModelStatus(m: CatalogModel) {
    onError(null)
    try {
      unwrap(
        await apiClient.PATCH('/model-catalog/providers/{id}/models/{model_id}', {
          params: { path: { id: providerId, model_id: m.id } },
          body: { status: m.status === 1 ? 2 : 1 },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['catalog-models', providerId] })
      queryClient.invalidateQueries({ queryKey: ['model-catalog'] })
    } catch (err) {
      onError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  async function deleteModel(m: CatalogModel) {
    onError(null)
    try {
      unwrap(
        await apiClient.DELETE('/model-catalog/providers/{id}/models/{model_id}', {
          params: { path: { id: providerId, model_id: m.id } },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['catalog-models', providerId] })
      queryClient.invalidateQueries({ queryKey: ['model-catalog'] })
    } catch (err) {
      onError(err instanceof ApiError ? err.message : '删除没能完成，请再试一次')
    }
  }

  const models = modelsQuery.data ?? []

  return (
    <div className="flex flex-col gap-space-3">
      <div className="flex items-center justify-between">
        <h3 className="text-label-md text-ink-700">这个 Provider 下的模型</h3>
        <Can permission="model_catalog.model.create">
          <Button variant="outline" size="sm" onClick={() => setCreateOpen(true)}>
            新增模型
          </Button>
        </Can>
      </div>

      {modelsQuery.isLoading && <ListSkeleton rows={2} />}
      {modelsQuery.isError && <ErrorPanel message="模型列表没能加载出来" onRetry={() => modelsQuery.refetch()} />}
      {modelsQuery.isSuccess && models.length === 0 && (
        <p className="text-body-sm text-ink-500">还没有添加模型，例如 deepseek-v3。</p>
      )}

      {models.length > 0 && (
        <ul className="overflow-hidden rounded-sm border border-border">
          {models.map((m) => (
            <li key={m.id} className="flex items-center gap-space-3 border-b border-border px-space-3 py-space-2 last:border-0">
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-center gap-space-2">
                  <Ref>{m.model}</Ref>
                  <span className="text-body-sm text-ink-900">{m.display_name}</span>
                  <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700">
                    {MODALITY_LABEL[m.modality]}
                  </span>
                  {m.featured && <span className="text-caption text-signal">精选</span>}
                </span>
                {m.description && <span className="text-caption truncate text-ink-500">{m.description}</span>}
              </span>
              <span className={cn('text-caption w-12 shrink-0 text-right', m.status === 1 ? 'text-moss' : 'text-ink-500')}>
                {m.status === 1 ? '已启用' : '已停用'}
              </span>
              <Can permission="model_catalog.model.toggle">
                <Button variant="outline" size="sm" onClick={() => toggleModelStatus(m)}>
                  {m.status === 1 ? '停用' : '启用'}
                </Button>
              </Can>
              <Can permission="model_catalog.model.delete">
                <Button variant="ghost" size="sm" onClick={() => deleteModel(m)}>
                  删除
                </Button>
              </Can>
            </li>
          ))}
        </ul>
      )}

      <CreateModelDialog
        providerId={providerId}
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={() => {
          queryClient.invalidateQueries({ queryKey: ['catalog-models', providerId] })
          queryClient.invalidateQueries({ queryKey: ['model-catalog'] })
          setCreateOpen(false)
        }}
      />
    </div>
  )
}

function CreateProviderDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: () => void
}) {
  const [key, setKey] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [icon, setIcon] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function reset() {
    setKey('')
    setDisplayName('')
    setIcon('')
    setBaseUrl('')
    setError(null)
  }

  async function handleIconFile(file: File | undefined) {
    if (!file) return
    if (file.size > MAX_ICON_BYTES) {
      setError('图标文件太大，请用 200KB 以内的图片')
      return
    }
    setIcon(await readFileAsDataURL(file))
  }

  async function submit() {
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.POST('/model-catalog/providers', {
          body: { key, display_name: displayName, icon: icon || undefined, base_url: baseUrl || undefined },
        }),
      )
      reset()
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新增 Provider</DialogTitle>
          <DialogDescription>登记后可以在它下面添加具体模型；启用的模型会出现在模型广场。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="provider-key" className="text-label-md text-ink-700">
            Key（英文标识，如 deepseek）
          </label>
          <Input id="provider-key" value={key} onChange={(e) => setKey(e.target.value)} className="h-12 rounded-sm" />

          <label htmlFor="provider-display-name" className="text-label-md text-ink-700">
            显示名称
          </label>
          <Input
            id="provider-display-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="h-12 rounded-sm"
          />

          <label htmlFor="provider-icon" className="text-label-md text-ink-700">
            图标（可选，200KB 以内）
          </label>
          <div className="flex items-center gap-space-3">
            {icon && <img src={icon} alt="" className="size-10 rounded-sm object-cover" />}
            <input
              id="provider-icon"
              type="file"
              accept="image/*"
              onChange={(e) => handleIconFile(e.target.files?.[0])}
              className="text-body-sm text-ink-700"
            />
          </div>

          <label htmlFor="provider-base-url" className="text-label-md text-ink-700">
            Base URL（可选）
          </label>
          <Input
            id="provider-base-url"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://api.example.com/v1"
            className="h-12 rounded-sm"
          />

          {error && (
            <p role="alert" className="text-caption text-rust">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending || !key || !displayName} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CreateModelDialog({
  providerId,
  open,
  onOpenChange,
  onCreated,
}: {
  providerId: string
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: () => void
}) {
  const [model, setModel] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [description, setDescription] = useState('')
  const [modality, setModality] = useState<CatalogModality>('text')
  const [featured, setFeatured] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function reset() {
    setModel('')
    setDisplayName('')
    setDescription('')
    setModality('text')
    setFeatured(false)
    setError(null)
  }

  async function submit() {
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.POST('/model-catalog/providers/{id}/models', {
          params: { path: { id: providerId } },
          body: { model, display_name: displayName, description: description || undefined, modality, featured },
        }),
      )
      reset()
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新增模型</DialogTitle>
          <DialogDescription>例如 deepseek-v3，标注它的类型，供模型广场按类型筛选。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="model-name" className="text-label-md text-ink-700">
            模型名称（如 deepseek-v3）
          </label>
          <Input id="model-name" value={model} onChange={(e) => setModel(e.target.value)} className="h-12 rounded-sm" />

          <label htmlFor="model-display-name" className="text-label-md text-ink-700">
            显示名称
          </label>
          <Input
            id="model-display-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="h-12 rounded-sm"
          />

          <label htmlFor="model-modality" className="text-label-md text-ink-700">
            类型
          </label>
          <Select value={modality} onValueChange={(v) => setModality(v as CatalogModality)}>
            <SelectTrigger id="model-modality" className="h-12 w-full rounded-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Object.entries(MODALITY_LABEL).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <label htmlFor="model-description" className="text-label-md text-ink-700">
            描述（可选）
          </label>
          <Textarea
            id="model-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
          />

          <label className="flex items-center gap-space-2 text-label-md text-ink-700">
            <Checkbox checked={featured} onCheckedChange={(v) => setFeatured(v === true)} />
            设为精选模型
          </label>

          {error && (
            <p role="alert" className="text-caption text-rust">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending || !model || !displayName} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
