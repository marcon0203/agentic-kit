import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
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
      <h1 className="text-headline-md text-ink-900">模型中心</h1>
      <p className="text-body-md max-w-[640px] text-ink-700">
        接入至少一个模型 Provider 后才能运行 Bundle；未接入时，全站的运行入口会被禁用。
      </p>

      {query.isLoading && <ListSkeleton rows={4} />}
      {query.isError && <ErrorPanel message="Provider 列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && (
        <div className="grid grid-cols-1 gap-space-5 md:grid-cols-2 lg:grid-cols-3">
          {PROVIDERS.map((p) => {
            const existing = connected.get(p.value)
            return (
              <Card key={p.value}>
                <CardHeader>
                  <CardTitle className="text-headline-sm flex items-center justify-between">
                    {p.label}
                    {existing && <Badge>已接入</Badge>}
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-space-3">
                  {existing ? (
                    <>
                      <p className="font-mono text-body-sm text-ink-500">sk-••••••••</p>
                      <Button variant="secondary" size="sm" onClick={() => setConnecting(p.value)}>
                        更换密钥
                      </Button>
                    </>
                  ) : (
                    <Button size="sm" onClick={() => setConnecting(p.value)}>
                      接入
                    </Button>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
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
            <p role="alert" className="text-caption text-[var(--color-error)]">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={pending}>
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
