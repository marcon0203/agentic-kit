import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, Plug } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { MarketAvatar } from '@/components/market/MarketAvatar'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MarketMCPServer = components['schemas']['MarketMCPServer']

/**
 * MCP 市场（外部注册中心）的卡片。
 *
 * 和 Skill 的市场卡片不同，这里没有"查看详情"再"安装"两步：MCP 条目的全部
 * 信息就是限定名、简介和一个远端地址，卡片上已经摆全了，再点进一页只是把
 * 同样几行字换个地方显示。所以接入按钮直接放在卡片上。
 *
 * 只提供本地运行包的条目（installable=false）按钮置灰并说明原因——平台连的
 * 是远端地址，装了也跑不起来。
 */
export function MarketMcpServerCard({ server }: { server: MarketMCPServer }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const installMutation = useMutation({
    mutationFn: async () =>
      unwrap<components['schemas']['Resource']>(
        await apiClient.POST('/mcp-market/{id}/install', { params: { path: { id: String(server.id) } } }),
      ),
    onSuccess: (created) => {
      toast.success(`已接入 ${created.ref}，去「自定义」里看连通性`)
      queryClient.invalidateQueries({ queryKey: ['resources', 'mcp'] })
      navigate('/apps/mcp')
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '接入没能完成，请再试一次'),
  })

  return (
    <div className="flex min-h-[9.5rem] flex-col gap-space-2 rounded-lg border border-border bg-surface p-space-4 transition-colors hover:border-border-strong">
      <span className="flex items-center gap-space-3">
        <MarketAvatar iconUrl={server.icon_url} seed={server.slug} name={server.name} />
        <span className="text-body-md min-w-0 truncate font-medium text-ink-900" title={server.slug}>
          {server.slug}
        </span>
      </span>
      <span className="text-body-sm line-clamp-2 text-ink-500">{server.summary || '上游没有提供简介。'}</span>

      {/* 地址摆在卡片上：接入之前，用户唯一该确认的就是"请求会发到哪去"。 */}
      {server.remote_url && (
        <span className="text-ref text-caption truncate text-ink-700" title={server.remote_url}>
          {server.remote_url}
        </span>
      )}

      <span className="text-caption mt-auto flex flex-wrap items-center gap-space-3 text-ink-500">
        {server.version && <span className="tabular">v{server.version}</span>}
        {server.remote_type && <span>{server.remote_type}</span>}
        <span className="truncate">来自 {server.source_name}</span>
        {server.repository_url && (
          <a
            href={server.repository_url}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1 text-blueprint hover:underline"
          >
            源码
            <ExternalLink className="size-3" aria-hidden />
          </a>
        )}
      </span>

      <div className="flex items-center gap-space-2">
        {server.installable ? (
          <Button size="sm" disabled={installMutation.isPending} onClick={() => installMutation.mutate()}>
            <Plug className="mr-1 size-3.5" aria-hidden />
            接入
          </Button>
        ) : (
          <span className="text-caption text-ink-500">只提供本地运行包，平台接入不了</span>
        )}
      </div>
    </div>
  )
}
