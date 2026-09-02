import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Blend, Clapperboard, Eye, Image as ImageIcon, Sparkles, Type } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ListSkeleton, ErrorPanel } from '@/components/common/EmptyState'
import { SectionSidebar, type SectionSidebarItem } from '@/components/layout/SectionSidebar'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ModelProvider = components['schemas']['ModelProvider']
type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']
type ProviderName = components['schemas']['ProviderName']
type Modality = ModelCatalogEntry['modality']

type ProviderSpec = components['schemas']['ModelProviderSpec']

/**
 * 渠道列表来自后端的 GET /model-provider-specs（后端的渠道注册表：内置的
 * 声明式渠道描述符 + 少数手写 client）。前端不再抄第二份——抄了的下场是每
 * 加一个渠道就有一处忘了改。
 *
 * 下面这份是加载中/请求失败时的兜底，只保证下拉框不是空的。
 */
const FALLBACK_SPECS: ProviderSpec[] = [
  { name: 'deepseek', label: 'DeepSeek', credentials: [] },
  { name: 'volcengine', label: '火山引擎方舟', credentials: [] },
  { name: 'qwen', label: '通义千问', credentials: [] },
  { name: 'custom', label: '自定义（OpenAI 兼容）', credentials: [] },
  { name: 'google', label: 'Google Gemini', credentials: [] },
]

function useProviderSpecs(): ProviderSpec[] {
  const query = useQuery({
    queryKey: ['model-provider-specs'],
    queryFn: async () =>
      unwrap<{ items: ProviderSpec[] }>(await apiClient.GET('/model-provider-specs', {})),
    staleTime: Infinity, // 渠道列表随二进制发布，一次会话里不会变
  })
  return query.data?.items?.length ? query.data.items : FALLBACK_SPECS
}

const KNOWN_PROVIDER_NAMES: ProviderName[] = FALLBACK_SPECS.map((s) => s.name)

// A catalog entry's provider is now a free-text key from 系统配置 → 模型
// 提供商 — an admin can register one that isn't among the 6 credentials
// modelcenter's connectivity probe knows how to validate. Falling back to
// 'custom' lets the connect dialog still open (with its base-url field)
// instead of crashing on an unrecognized key.
function toProviderName(key: string): ProviderName {
  return (KNOWN_PROVIDER_NAMES as string[]).includes(key) ? (key as ProviderName) : 'custom'
}

const MODALITIES: SectionSidebarItem[] = [
  { value: 'all', label: '全部类型', icon: Sparkles },
  { value: 'text', label: '文本模型', icon: Type },
  { value: 'image', label: '图片模型', icon: ImageIcon },
  { value: 'video', label: '视频模型', icon: Clapperboard },
  { value: 'vision', label: '图文理解', icon: Eye },
  { value: 'embedding', label: '向量模型', icon: Blend },
]

/**
 * 模型广场：左侧按模型类型筛选，顶部再按供应商筛选、精选/全部切换，内容区
 * 按供应商分组展示目录里的模型卡片。目录（GET /model-catalog）读取的是
 * 系统配置 → 模型提供商里管理员登记的 Provider + Model，是展示数据——
 * Agent 的 model.name 仍然是自由文本，这里不是在绑定一个可选值列表，是在
 * 帮用户挑一个起点。真正决定"能不能用"的还是右上角"新增模型"接入的
 * Provider 凭证。
 */
export function ModelProviderPage() {
  const queryClient = useQueryClient()
  const [modality, setModality] = useState('all')
  const [providerFilter, setProviderFilter] = useState('all')
  const [tab, setTab] = useState<'featured' | 'all'>('featured')
  const [connecting, setConnecting] = useState<ProviderName | null>(null)

  const providersQuery = useQuery({
    queryKey: ['model-providers'],
    queryFn: async () => unwrap<ModelProvider[]>(await apiClient.GET('/model-providers', {})),
  })
  const catalogQuery = useQuery({
    queryKey: ['model-catalog'],
    queryFn: async () => unwrap<ModelCatalogEntry[]>(await apiClient.GET('/model-catalog', {})),
  })

  const connected = new Set((providersQuery.data ?? []).map((p) => p.provider))

  const providerOptions = useMemo(() => {
    const seen = new Map<string, string>()
    for (const e of catalogQuery.data ?? []) seen.set(e.provider, e.provider_display_name)
    return Array.from(seen.entries())
  }, [catalogQuery.data])

  const grouped = useMemo(() => {
    const items = (catalogQuery.data ?? [])
      .filter((e) => tab === 'all' || e.featured)
      .filter((e) => modality === 'all' || e.modality === (modality as Modality))
      .filter((e) => providerFilter === 'all' || e.provider === providerFilter)
    const byProvider = new Map<string, { displayName: string; icon?: string; items: ModelCatalogEntry[] }>()
    for (const e of items) {
      const bucket = byProvider.get(e.provider) ?? { displayName: e.provider_display_name, icon: e.provider_icon, items: [] }
      bucket.items.push(e)
      byProvider.set(e.provider, bucket)
    }
    return Array.from(byProvider.entries())
  }, [catalogQuery.data, tab, modality, providerFilter])

  return (
    <div className="flex flex-1 flex-col gap-space-6">
      <div className="flex flex-1 flex-col gap-space-6 sm:flex-row">
        <SectionSidebar items={MODALITIES} active={modality} onChange={setModality} />

        <div className="min-w-0 flex-1 rounded-lg border border-border bg-surface p-space-6">
          <div className="mb-space-5 flex flex-wrap items-center justify-between gap-space-3">
            <div role="tablist" className="flex w-fit items-center gap-space-1 rounded-full border border-border bg-surface-muted p-1">
              {(['featured', 'all'] as const).map((t) => (
                <button
                  key={t}
                  type="button"
                  role="tab"
                  aria-selected={tab === t}
                  onClick={() => setTab(t)}
                  className={cn(
                    'text-body-sm rounded-full px-space-4 py-1.5 transition-colors',
                    tab === t ? 'bg-surface text-ink-900 shadow-sm' : 'text-ink-500 hover:text-ink-900',
                  )}
                >
                  {t === 'featured' ? '精选模型' : '全部模型'}
                </button>
              ))}
            </div>

            <div className="flex items-center gap-space-3">
              {providerOptions.length > 0 && (
                <Select value={providerFilter} onValueChange={setProviderFilter}>
                  <SelectTrigger className="h-9 w-[180px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部供应商</SelectItem>
                    {providerOptions.map(([key, label]) => (
                      <SelectItem key={key} value={key}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setConnecting('deepseek')}>
                新增模型
              </Button>
            </div>
          </div>

          {(catalogQuery.isLoading || providersQuery.isLoading) && <ListSkeleton rows={4} />}
          {(catalogQuery.isError || providersQuery.isError) && (
            <ErrorPanel message="模型广场没能加载出来" onRetry={() => { catalogQuery.refetch(); providersQuery.refetch() }} />
          )}

          {catalogQuery.isSuccess && grouped.length === 0 && (
            <p className="text-body-sm text-ink-500">没有匹配当前筛选条件的模型。</p>
          )}

          {catalogQuery.isSuccess && (
            <div className="flex flex-col gap-space-7">
              {grouped.map(([providerKey, group]) => (
                <div key={providerKey} className="flex flex-col gap-space-3">
                  <h3 className="text-label-md flex items-center gap-space-2 text-ink-900">
                    {group.icon && <img src={group.icon} alt="" className="size-5 rounded-sm object-cover" />}
                    {group.displayName}
                  </h3>
                  <div className="grid grid-cols-1 gap-space-3 md:grid-cols-2 xl:grid-cols-3">
                    {group.items.map((entry) => {
                      const providerName = toProviderName(entry.provider)
                      const isConnected = connected.has(providerName)
                      return (
                        <button
                          key={`${entry.provider}/${entry.model}`}
                          type="button"
                          onClick={() => !isConnected && setConnecting(providerName)}
                          className="flex flex-col gap-space-2 rounded-lg border border-border bg-surface p-space-4 text-left transition-colors hover:border-border-strong"
                        >
                          <div className="flex items-center justify-between gap-space-2">
                            <span className="text-body-md font-medium text-ink-900">{entry.display_name}</span>
                            {isConnected ? (
                              <span className="text-caption inline-flex shrink-0 items-center gap-1 text-moss">
                                <span aria-hidden className="size-1.5 rounded-full bg-moss" />
                                已接入
                              </span>
                            ) : (
                              <span className="text-caption shrink-0 text-ink-500">未接入</span>
                            )}
                          </div>
                          <span className="text-caption text-ink-500">
                            {group.displayName} · {entry.model}
                          </span>
                          <span className="text-body-sm text-ink-700">{entry.description}</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {connecting && (
        <ConnectProviderDialog
          provider={connecting}
          open={!!connecting}
          onOpenChange={(v) => !v && setConnecting(null)}
          onConnected={() => {
            queryClient.invalidateQueries({ queryKey: ['model-providers'] })
            setConnecting(null)
          }}
        />
      )}
    </div>
  )
}

function ConnectProviderDialog({
  provider,
  open,
  onOpenChange,
  onConnected,
}: {
  provider: ProviderName
  open: boolean
  onOpenChange: (v: boolean) => void
  onConnected: () => void
}) {
  const specs = useProviderSpecs()
  const [selectedProvider, setSelectedProvider] = useState(provider)
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  // 没有默认地址的渠道（custom）必须自己填 base_url。这条判据来自后端声明
  // 的 default_base_url，不再在前端硬编渠道名。
  const requiresBaseUrl = specs.find((s) => s.name === selectedProvider)?.default_base_url === ''

  async function submit() {
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.POST('/model-providers', {
          body: { provider: selectedProvider, api_key: apiKey, base_url: baseUrl || undefined },
        }),
      )
      setApiKey('')
      setBaseUrl('')
      onConnected()
    } catch (err) {
      // Backend does a live connectivity probe before saving — surfaces
      // the real rejection reason (spec-15: "非泛泛的接入失败").
      setError(err instanceof ApiError ? err.message : '接入失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新增模型 Provider</DialogTitle>
          <DialogDescription>API Key 加密存储，保存后不会再回显完整内容。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="provider-select" className="text-label-md text-ink-700">
            Provider
          </label>
          <Select value={selectedProvider} onValueChange={(v) => setSelectedProvider(v as ProviderName)}>
            <SelectTrigger id="provider-select" className="h-12 w-full rounded-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {specs.map((spec) => (
                <SelectItem key={spec.name} value={spec.name}>
                  {spec.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <label htmlFor="provider-api-key" className="text-label-md text-ink-700">
            API Key
          </label>
          <Input
            id="provider-api-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            className="h-12 rounded-sm"
          />
          <label htmlFor="provider-base-url" className="text-label-md text-ink-700">
            Base URL{requiresBaseUrl ? '' : '（可选，留空使用官方地址）'}
          </label>
          <Input
            id="provider-base-url"
            type="text"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder={requiresBaseUrl ? 'https://your-endpoint.example.com/v1' : ''}
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
          <Button disabled={pending || !apiKey || (requiresBaseUrl && !baseUrl)} onClick={submit}>
            {pending ? '连接测试中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
