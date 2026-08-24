import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Blend, Eye, Sparkles, Type } from 'lucide-react'

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
import { PageHeader } from '@/components/common/Page'
import { SectionSidebar, type SectionSidebarItem } from '@/components/layout/SectionSidebar'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ModelProvider = components['schemas']['ModelProvider']
type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']
type ProviderName = components['schemas']['ProviderName']
type Modality = ModelCatalogEntry['modality']
type Category = ModelCatalogEntry['category']

const PROVIDER_LABEL: Record<ProviderName, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  google: 'Google',
  deepseek: 'DeepSeek',
  qwen: '通义千问',
  custom: '自定义',
}

const MODALITIES: SectionSidebarItem[] = [
  { value: 'all', label: '全部模态', icon: Sparkles },
  { value: 'text', label: '文本模型', icon: Type },
  { value: 'vision', label: '视觉模型', icon: Eye },
  { value: 'embedding', label: '向量模型', icon: Blend },
]

const CATEGORY_LABEL: Record<Category, string> = {
  reasoning: '深度思考',
  text: '文本生成',
  vision: '视觉理解',
  embedding: '向量模型',
}
const CATEGORY_ORDER: Category[] = ['reasoning', 'text', 'vision', 'embedding']

/**
 * 模型广场：左侧按模态筛选（文本/视觉/向量），顶部精选/全部两个 Tab，内容区
 * 按用途分组展示目录里的模型卡片。目录（GET /model-catalog）只是展示数据——
 * Agent 的 model.name 仍然是自由文本，这里不是在绑定一个可选值列表，是在
 * 帮用户挑一个起点。真正决定"能不能用"的还是右上角"新增模型"接入的
 * Provider 凭证。
 */
export function ModelProviderPage() {
  const queryClient = useQueryClient()
  const [modality, setModality] = useState('all')
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

  const grouped = useMemo(() => {
    const items = (catalogQuery.data ?? [])
      .filter((e) => tab === 'all' || e.featured)
      .filter((e) => modality === 'all' || e.modality === (modality as Modality))
    const byCategory = new Map<Category, ModelCatalogEntry[]>()
    for (const e of items) {
      const list = byCategory.get(e.category) ?? []
      list.push(e)
      byCategory.set(e.category, list)
    }
    return CATEGORY_ORDER.map((c) => ({ category: c, items: byCategory.get(c) ?? [] })).filter(
      (g) => g.items.length > 0,
    )
  }, [catalogQuery.data, tab, modality])

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader
        eyebrow="MODEL PROVIDERS"
        title="模型广场"
        description="按模态挑一个模型作为起点；真正能不能用取决于右边接入的 Provider 凭证——密钥先拿去真实验证一次再保存。"
        actions={
          <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setConnecting('anthropic')}>
            新增模型
          </Button>
        }
      />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar items={MODALITIES} active={modality} onChange={setModality} />

        <div className="min-w-0 flex-1">
          <div
            role="tablist"
            className="mb-space-5 flex w-fit items-center gap-space-1 rounded-full border border-border bg-surface-muted p-1"
          >
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

          {(catalogQuery.isLoading || providersQuery.isLoading) && <ListSkeleton rows={4} />}
          {(catalogQuery.isError || providersQuery.isError) && (
            <ErrorPanel message="模型广场没能加载出来" onRetry={() => { catalogQuery.refetch(); providersQuery.refetch() }} />
          )}

          {catalogQuery.isSuccess && (
            <div className="flex flex-col gap-space-7">
              {grouped.map((group) => (
                <div key={group.category} className="flex flex-col gap-space-3">
                  <h3 className="text-label-md text-ink-900">{CATEGORY_LABEL[group.category]}</h3>
                  <div className="grid grid-cols-1 gap-space-3 md:grid-cols-2 xl:grid-cols-3">
                    {group.items.map((entry) => {
                      const isConnected = connected.has(entry.provider)
                      return (
                        <button
                          key={`${entry.provider}/${entry.model}`}
                          type="button"
                          onClick={() => !isConnected && setConnecting(entry.provider)}
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
                            {PROVIDER_LABEL[entry.provider]} · {entry.model}
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
  const [selectedProvider, setSelectedProvider] = useState(provider)
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const requiresBaseUrl = selectedProvider === 'custom'

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
              {Object.entries(PROVIDER_LABEL).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
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
