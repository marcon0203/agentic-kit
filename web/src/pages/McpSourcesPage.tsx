import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Server, Trash2 } from 'lucide-react'
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
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MCPSource = components['schemas']['MCPSource']

/** 官方注册中心。新建弹窗直接填好，省得管理员去查地址。 */
const OFFICIAL_REGISTRY = 'https://registry.modelcontextprotocol.io'

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
  const [syncingId, setSyncingId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState<MCPSource | null>(null)

  const query = useQuery({
    queryKey: ['mcp-sources'],
    queryFn: async () => unwrap<{ items: MCPSource[] }>(await apiClient.GET('/mcp-sources', {})),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['mcp-sources'] })

  function openAdd() {
    setName('')
    setBaseUrl(OFFICIAL_REGISTRY)
    setAddOpen(true)
  }

  const createMutation = useMutation({
    mutationFn: async () =>
      unwrap<MCPSource>(
        await apiClient.POST('/mcp-sources', { body: { name: name.trim(), base_url: baseUrl.trim() } }),
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

  const items = query.data?.items ?? []

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
                  <span className="tabular">{s.server_count} 个 Server</span>
                  <span>{formatSyncedAt(s.last_synced_at ?? null)}</span>
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
            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">名称</span>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="官方 MCP Registry" />
            </label>
            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">地址</span>
              <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={OFFICIAL_REGISTRY} />
            </label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddOpen(false)}>
              取消
            </Button>
            <Button
              className="bg-gradient-cta text-white hover:opacity-90"
              disabled={createMutation.isPending || !name.trim() || !baseUrl.trim()}
              onClick={() => createMutation.mutate()}
            >
              登记
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
