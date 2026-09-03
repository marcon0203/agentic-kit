import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RefreshCw, Server, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Ref, Section } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MCPSource = components['schemas']['MCPSource']
type MCPSourceProtocol = components['schemas']['MCPSourceProtocol']
type ProtocolID = components['schemas']['MCPSourceProtocolID']

function formatSyncedAt(iso: string | null): string {
  if (!iso) return '从未同步'
  const d = new Date(iso)
  return `${d.toLocaleDateString()} ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

/**
 * 系统配置 → MCP 源：管理员登记公开 MCP 注册中心（如官方
 * registry.modelcontextprotocol.io），手动同步后其公开 Server 经审核进入
 * MCP 管理 → 市场视图。同步失败不静默——last_sync_error 原样展示在这里。
 *
 * 和 Skill 源是同一套流程、同一套页面结构，刻意不抽公共组件：两边的条目字
 * 段差别不小（Skill 有 stars/downloads，MCP 有远端地址和可接入判定），抽出
 * 来的"通用源页面"会立刻长满 if。
 */
export function McpSourcesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [protocol, setProtocol] = useState<ProtocolID>('mcp-registry')
  const [apiPrefix, setApiPrefix] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [syncingId, setSyncingId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState<MCPSource | null>(null)
  const [rekeying, setRekeying] = useState<MCPSource | null>(null)
  const [newKey, setNewKey] = useState('')

  const query = useQuery({
    queryKey: ['mcp-sources'],
    queryFn: async () => unwrap<{ items: MCPSource[] }>(await apiClient.GET('/mcp-sources', {})),
  })

  // 协议清单来自后端（默认地址、默认前缀、要不要密钥都在里面），前端不再
  // 抄第二份——加一家源只改后端那张表。
  const protocolsQuery = useQuery({
    queryKey: ['mcp-source-protocols'],
    queryFn: async () =>
      unwrap<{ items: MCPSourceProtocol[] }>(await apiClient.GET('/mcp-source-protocols', {})),
  })
  const protocols = protocolsQuery.data?.items ?? []
  const activeProtocol = protocols.find((p) => p.id === protocol)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['mcp-sources'] })

  // 换协议就把地址和前缀重填成该协议的默认值：这两项跟着协议走，留着上一
  // 个协议的地址只会让人以为填过了。
  function pickProtocol(p: MCPSourceProtocol) {
    setProtocol(p.id)
    setBaseUrl(p.default_base_url ?? '')
    setApiPrefix(p.default_api_prefix ?? '')
    setApiKey('')
  }

  function openAdd() {
    setName('')
    setApiKey('')
    const first = protocols[0]
    if (first) pickProtocol(first)
    else {
      setProtocol('mcp-registry')
      setBaseUrl('')
      setApiPrefix('')
    }
    setAddOpen(true)
  }

  const createMutation = useMutation({
    mutationFn: async () =>
      unwrap<MCPSource>(
        await apiClient.POST('/mcp-sources', {
          body: {
            name: name.trim(),
            base_url: baseUrl.trim(),
            protocol,
            api_prefix: apiPrefix.trim(),
            ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
          },
        }),
      ),
    onSuccess: (src) => {
      toast.success(`已登记 ${src.name}，点"同步"拉取它的公开 MCP Server`)
      setAddOpen(false)
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '登记没能完成，请再试一次'),
  })

  const syncMutation = useMutation({
    mutationFn: async (id: number) =>
      unwrap<MCPSource>(await apiClient.POST('/mcp-sources/{id}/sync', { params: { path: { id: String(id) } } })),
    onMutate: (id) => setSyncingId(id),
    onSettled: () => setSyncingId(null),
    onSuccess: (src) => {
      if (src.last_sync_error) {
        toast.error(`同步失败：${src.last_sync_error}`)
      } else {
        toast.success(`同步完成，缓存了 ${src.server_count} 个 MCP Server`)
      }
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '同步没能完成，请再试一次'),
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      await apiClient.DELETE('/mcp-sources/{id}', { params: { path: { id: String(id) } } })
    },
    onSuccess: () => {
      toast.success('已删除该源及其缓存')
      setDeleting(null)
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '删除没能完成，请再试一次'),
  })

  const rekeyMutation = useMutation({
    mutationFn: async ({ id, key }: { id: number; key: string }) => {
      unwrap(
        await apiClient.PUT('/mcp-sources/{id}/api-key', {
          params: { path: { id: String(id) } },
          body: { api_key: key },
        }),
      )
    },
    onSuccess: () => {
      toast.success('已更新 API Key，重新同步一次试试')
      setRekeying(null)
      setNewKey('')
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '更新没能完成，请再试一次'),
  })

  const items = query.data?.items ?? []
  const labelOfProtocol = (id: string) => protocols.find((p) => p.id === id)?.label ?? id

  return (
    <Section
      title="MCP 源"
      aside={
        <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={openAdd}>
          添加源
        </Button>
      }
    >
      {query.isLoading && <ListSkeleton rows={3} />}
      {query.isError && <ErrorPanel message="源列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyRail
          title="登记第一个 MCP 源"
          description="填一个公开 MCP 注册中心的根地址（官方是 https://registry.modelcontextprotocol.io），同步后它的 Server 经你审核会出现在 MCP 管理的市场视图里，用户点一下就能接入。"
          action={
            <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={openAdd}>
              添加源
            </Button>
          }
        />
      )}

      {items.length > 0 && (
        <ul className="overflow-hidden rounded-lg border border-border bg-surface">
          {items.map((s) => (
            <li
              key={s.id}
              className="flex flex-wrap items-center gap-space-4 border-b border-border px-space-5 py-space-4 last:border-0"
            >
              <span
                aria-hidden
                className="text-ink-500 flex size-9 shrink-0 items-center justify-center rounded-md bg-surface-muted"
              >
                <Server className="size-4" />
              </span>
              <div className="flex min-w-0 flex-1 flex-col gap-space-1">
                <span className="flex flex-wrap items-center gap-space-3">
                  {/* 源名进详情：那个源同步下来的 Server 在详情里逐条/批量审核。 */}
                  <button
                    type="button"
                    className="text-body-md font-medium text-ink-900 hover:text-blueprint hover:underline"
                    onClick={() => navigate(`/settings/mcp-sources/${s.id}`)}
                  >
                    {s.name}
                  </button>
                  <a
                    href={s.base_url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-body-sm truncate text-blueprint hover:underline"
                  >
                    {s.base_url}
                  </a>
                </span>
                <span className="text-caption flex flex-wrap items-center gap-space-3 text-ink-500">
                  <span className="rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700">
                    {labelOfProtocol(s.protocol)}
                  </span>
                  {s.api_prefix && <span className="text-ref">{s.api_prefix}</span>}
                  <span className="tabular">{s.server_count} 个 Server</span>
                  <span>{formatSyncedAt(s.last_synced_at ?? null)}</span>
                  {s.has_api_key && <span className="text-moss">已配 API Key</span>}
                </span>
                {s.last_sync_error && (
                  <span role="alert" className="text-caption text-rust">
                    上次同步失败：{s.last_sync_error}
                  </span>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-space-2">
                <Button variant="outline" size="sm" onClick={() => navigate(`/settings/mcp-sources/${s.id}`)}>
                  审核 Server
                </Button>
                {s.has_api_key && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setNewKey('')
                      setRekeying(s)
                    }}
                  >
                    <KeyRound aria-hidden />
                    换密钥
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  disabled={syncMutation.isPending && syncingId === Number(s.id)}
                  onClick={() => syncMutation.mutate(Number(s.id))}
                >
                  <RefreshCw
                    className={syncMutation.isPending && syncingId === Number(s.id) ? 'animate-spin' : ''}
                    aria-hidden
                  />
                  同步
                </Button>
                <Button variant="outline" size="sm" onClick={() => setDeleting(s)}>
                  <Trash2 aria-hidden />
                  删除
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>添加 MCP 源</DialogTitle>
            <DialogDescription>
              MCP 注册中心的根地址，不带路径。登记后点"同步"拉取它的公开 Server。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-space-4">
            {/* 先选协议：地址、前缀、要不要密钥都跟着它变。 */}
            <div className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">协议</span>
              <div className="flex flex-col gap-space-2">
                {protocols.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => pickProtocol(p)}
                    className={cn(
                      'flex flex-col items-start gap-0.5 rounded-md border px-space-3 py-space-2 text-left transition-colors',
                      p.id === protocol
                        ? 'border-blueprint bg-blueprint-tint'
                        : 'border-border hover:border-border-strong',
                    )}
                  >
                    <span className="text-body-sm font-medium text-ink-900">{p.label}</span>
                    {p.description && <span className="text-caption text-ink-500">{p.description}</span>}
                  </button>
                ))}
              </div>
            </div>

            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">名称</span>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="官方 MCP Registry" />
            </label>

            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">地址</span>
              <Input
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                placeholder="https://registry.example.com"
              />
            </label>

            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">接口版本前缀</span>
              <Input value={apiPrefix} onChange={(e) => setApiPrefix(e.target.value)} placeholder="/v0" />
              {/* 前缀填错是这套东西最常见的故障，所以把最终请求路径直接摆出
                  来给管理员对，而不是等同步失败了再去猜。 */}
              <span className="text-caption text-ink-500">
                实际会请求 <span className="text-ref">{(baseUrl || 'https://…') + apiPrefix + '/servers'}</span>
                {activeProtocol?.docs_url && (
                  <>
                    ，对不上就查{' '}
                    <a
                      href={activeProtocol.docs_url}
                      target="_blank"
                      rel="noreferrer noopener"
                      className="text-blueprint hover:underline"
                    >
                      对方文档
                    </a>
                  </>
                )}
              </span>
            </label>

            {activeProtocol?.requires_api_key && (
              <label className="flex flex-col gap-space-2">
                <span className="text-label-md text-ink-900">API Key</span>
                <Input
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder="在对方站点申请"
                />
                <span className="text-caption text-ink-500">加密保存，任何页面和接口都不会再显示出来。</span>
              </label>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddOpen(false)}>
              取消
            </Button>
            <Button
              className="bg-gradient-cta text-white hover:opacity-90"
              disabled={
                createMutation.isPending ||
                !name.trim() ||
                !baseUrl.trim() ||
                (activeProtocol?.requires_api_key === true && !apiKey.trim())
              }
              onClick={() => createMutation.mutate()}
            >
              登记
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={rekeying !== null} onOpenChange={(open) => !open && setRekeying(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>更换 API Key</DialogTitle>
            <DialogDescription>
              {rekeying && (
                <>
                  给 <Ref>{rekeying.name}</Ref> 换一个新密钥。已同步的条目和审核结论都不受影响。
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <label className="flex flex-col gap-space-2">
            <span className="text-label-md text-ink-900">新的 API Key</span>
            <Input type="password" value={newKey} onChange={(e) => setNewKey(e.target.value)} />
          </label>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRekeying(null)}>
              取消
            </Button>
            <Button
              disabled={rekeyMutation.isPending || !newKey.trim()}
              onClick={() => rekeying && rekeyMutation.mutate({ id: Number(rekeying.id), key: newKey.trim() })}
            >
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除 MCP 源</DialogTitle>
            <DialogDescription>
              {deleting && (
                <>
                  确认删除 <Ref>{deleting.name}</Ref>
                  ？它缓存的 {deleting.server_count} 个 Server 会一并从市场视图里消失。已经接入成资源的不受影响。
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={() => deleting && deleteMutation.mutate(Number(deleting.id))}
            >
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  )
}
