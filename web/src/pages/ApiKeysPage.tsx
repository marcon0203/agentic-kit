import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, Copy, KeyRound } from 'lucide-react'

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
import { EmptyState, ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ApiKeySummary = components['schemas']['ApiKeySummary']
type ApiKeyCreated = components['schemas']['ApiKeyCreated']

function formatDateTime(iso: string): string {
  const d = new Date(iso)
  return `${d.toLocaleDateString()} ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

/**
 * 系统配置 → API Key 管理：第三方 / 脚本调用本平台 API 用的个人凭证——
 * 发布一个应用之后，客户接的 Open API 就是拿这里发的 key 鉴权
 * （`Authorization: ApiKey <key>`），和登录态的 JWT 并行有效。
 *
 * api_keys 表和它的查表逻辑（AuthMiddleware）本来就在，缺的只是一个能
 * 创建/查看/吊销的入口——这个页面就是补上这个入口。
 */
export function ApiKeysPage() {
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [pending, setPending] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [justCreated, setJustCreated] = useState<ApiKeyCreated | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['api-keys'],
    queryFn: async () => unwrap<ApiKeySummary[]>(await apiClient.GET('/api-keys', {})),
  })

  async function create() {
    setPending(true)
    setCreateError(null)
    try {
      const created = unwrap<ApiKeyCreated>(
        await apiClient.POST('/api-keys', { body: { name: name.trim() } }),
      )
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      setCreateOpen(false)
      setName('')
      setJustCreated(created)
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : '创建失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  async function revoke(key: ApiKeySummary) {
    setActionError(null)
    try {
      assertOk(await apiClient.DELETE('/api-keys/{id}', { params: { path: { id: key.id } } }))
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      toast.success('已吊销')
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '吊销失败，请稍后重试')
    }
  }

  const keys = query.data ?? []
  const activeCount = keys.filter((k) => !k.revoked).length

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex items-start justify-between gap-space-4">
        <p className="text-body-sm max-w-[560px] text-ink-500">
          第三方系统调用本平台 API 用的个人凭证——发布应用之后，无论是客户自己接 Open
          API，还是拿来写一个调用脚本，都用这里创建的 key 鉴权：
          <code className="text-ref-sm mx-1 rounded-xs bg-surface-muted px-1 py-0.5">
            Authorization: ApiKey &lt;key&gt;
          </code>
          ，和登录态并行有效。
        </p>
        <Button className="shrink-0 bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
          新建 API Key
        </Button>
      </div>

      {actionError && (
        <p role="alert" className="text-body-sm text-rust">
          {actionError}
        </p>
      )}

      {query.isLoading && <ListSkeleton />}
      {query.isError && <ErrorPanel message="API Key 列表加载失败" onRetry={() => query.refetch()} />}

      {query.isSuccess && keys.length === 0 && (
        <EmptyState
          title="还没有创建任何 API Key"
          description="创建一个之后，第三方系统就能拿它调用你发布的应用。"
          action={
            <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
              新建第一个
            </Button>
          }
        />
      )}

      {keys.length > 0 && (
        <ul className="flex flex-col gap-space-2">
          {keys.map((k) => (
            <li key={k.id} className="flex items-center gap-space-4 rounded-md border border-border bg-surface px-space-5 py-space-4">
              <KeyRound className="size-4 shrink-0 text-ink-500" aria-hidden />
              <div className="flex-1">
                <div className="flex items-center gap-space-2">
                  <span className="text-body-md text-ink-900">{k.name}</span>
                  {k.revoked && (
                    <span className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-500">已吊销</span>
                  )}
                </div>
                <p className="text-body-sm text-ink-500">
                  创建于 {formatDateTime(k.created_at)}
                  {k.last_used_at ? ` · 最后使用于 ${formatDateTime(k.last_used_at)}` : ' · 还没有被使用过'}
                </p>
              </div>
              {!k.revoked && (
                <Button variant="outline" size="sm" onClick={() => revoke(k)}>
                  吊销
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {activeCount > 0 && (
        <p className="text-caption text-ink-500">当前 {activeCount} 个有效 key。</p>
      )}

      {/* 新建 */}
      <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (!open) setCreateError(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建 API Key</DialogTitle>
            <DialogDescription>取一个能想起用途的名字——比如接进哪个系统、哪个脚本在用。</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-space-2 py-space-2">
            <label htmlFor="apikey-name" className="text-label-md text-ink-700">
              名称
            </label>
            <Input
              id="apikey-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="第三方系统集成"
              className="h-12 rounded-sm"
              autoFocus
            />
          </div>
          {createError && (
            <p role="alert" className="text-body-sm text-rust">
              {createError}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              取消
            </Button>
            <Button disabled={pending || !name.trim()} onClick={create}>
              {pending ? '创建中…' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 创建成功：原文只有这一次能看到 */}
      <Dialog open={!!justCreated} onOpenChange={(open) => !open && setJustCreated(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>已创建：{justCreated?.name}</DialogTitle>
            <DialogDescription>
              这个密钥只会显示这一次，关掉这个弹窗之后就再也看不到原文了——先复制好，保存到一个安全的地方。
            </DialogDescription>
          </DialogHeader>
          {justCreated && <RawKeyDisplay rawKey={justCreated.raw_key} />}
          <DialogFooter>
            <Button onClick={() => setJustCreated(null)}>我已经保存好了</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function RawKeyDisplay({ rawKey }: { rawKey: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(rawKey)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // 剪贴板不可用（非 HTTPS、权限被拒）时，密钥本身仍然可见可手动选中复制。
    }
  }

  return (
    <div className="flex items-center gap-space-2 rounded-md border border-border bg-surface-muted px-space-4 py-space-3">
      <code className="text-ref-sm flex-1 overflow-x-auto whitespace-nowrap text-ink-900">{rawKey}</code>
      <Button variant="outline" size="sm" onClick={copy} className="shrink-0">
        {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
        {copied ? '已复制' : '复制'}
      </Button>
    </div>
  )
}
