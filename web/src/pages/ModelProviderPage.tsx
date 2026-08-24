import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ModelProvider = components['schemas']['ModelProvider']
type ProviderName = components['schemas']['ProviderName']

const PROVIDERS: { value: ProviderName; label: string }[] = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'google', label: 'Google' },
  { value: 'custom', label: '自定义' },
]

export function ModelProviderPage() {
  const queryClient = useQueryClient()
  const [connecting, setConnecting] = useState<ProviderName | null>(null)

  const query = useQuery({
    queryKey: ['model-providers'],
    queryFn: async () => unwrap<ModelProvider[]>(await apiClient.GET('/model-providers', {})),
  })

  const connected = new Map((query.data ?? []).map((p) => [p.provider, p]))

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader
        eyebrow="MODEL PROVIDERS"
        title="模型广场"
        description="Bundle 要跑起来，至少得接入一个 Provider。密钥先拿去真实验证一次再保存，所以存进来的 key 一定是能用的。"
      />

      {query.isLoading && <ListSkeleton rows={4} />}
      {query.isError && (
        <ErrorPanel message="Provider 列表没能加载出来" onRetry={() => query.refetch()} />
      )}

      {query.isSuccess && (
        <ul className="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-2">
          {PROVIDERS.map((p) => {
            const existing = connected.get(p.value)
            return (
              <li
                key={p.value}
                className="flex items-center justify-between gap-space-4 bg-surface px-space-5 py-space-4"
              >
                <span className="flex min-w-0 flex-col gap-1">
                  <span className="flex items-center gap-space-3">
                    <span
                      aria-hidden
                      className={cn(
                        'size-2 shrink-0 rounded-full',
                        existing ? 'bg-moss' : 'bg-border-strong',
                      )}
                    />
                    <span className="text-display-sm text-ink-900">{p.label}</span>
                  </span>
                  <span className="text-caption pl-space-5 text-ink-500">
                    {existing ? '已接入，密钥加密保存，不再回显' : '尚未接入'}
                  </span>
                </span>
                <Button
                  variant={existing ? 'outline' : 'default'}
                  size="sm"
                  className={cn(!existing && 'bg-gradient-cta text-white hover:opacity-90')}
                  onClick={() => setConnecting(p.value)}
                >
                  {existing ? '更换密钥' : '接入'}
                </Button>
              </li>
            )
          })}
        </ul>
      )}

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
  const [apiKey, setApiKey] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  async function submit() {
    setPending(true)
    setError(null)
    try {
      unwrap(await apiClient.POST('/model-providers', { body: { provider, api_key: apiKey } }))
      setApiKey('')
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
          <DialogTitle>接入 {provider}</DialogTitle>
          <DialogDescription>API Key 加密存储，保存后不会再回显完整内容。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
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
          <Button disabled={pending || !apiKey} onClick={submit}>
            {pending ? '连接测试中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
