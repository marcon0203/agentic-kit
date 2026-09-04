import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronUp, Flag, Lock, Play, Sparkles, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'

import { Ref } from '@/components/common/Page'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { SubscribeDialog } from '@/components/marketplace/SubscribeDialog'
import { ApiUsagePanel } from '@/components/marketplace/ApiUsagePanel'
import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type ListingDetail = components['schemas']['ListingDetail']

/** 展示区块的通用外壳——和 MyListingsPage/AgentCard 那批列表页同一套扁平
 * 卡片语言（rounded-lg border + bg-surface），不用 shadcn 的 Card：那套带
 * 阴影/更重的分割线，跟这个应用里其它页面的视觉不是一回事，混在一起看
 * 起来就是这页比别的页面"乱"的地方。 */
function Section({ title, icon, children }: { title: string; icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-border bg-surface p-space-6">
      <h2 className="text-display-sm mb-space-4 flex items-center gap-space-2 text-ink-900">
        {icon}
        {title}
      </h2>
      {children}
    </section>
  )
}

export function ListingDetailPage() {
  const { ref } = useParams<{ ref: string }>()
  const userId = useAuthStore((s) => s.user?.id)
  const [subscribeOpen, setSubscribeOpen] = useState(false)
  const [reportOpen, setReportOpen] = useState(false)
  const [reportReason, setReportReason] = useState('')
  const [reportError, setReportError] = useState<string | null>(null)
  const [apiPanelOpen, setApiPanelOpen] = useState(false)
  const queryClient = useQueryClient()

  const submitReport = useMutation({
    mutationFn: async () => {
      assertOk(
        await apiClient.POST('/marketplace/listings/{ref}/report', {
          params: { path: { ref: ref! } },
          body: { reason: reportReason },
        }),
      )
    },
    onSuccess: () => {
      toast('举报已提交，运营团队会尽快处理')
      setReportOpen(false)
      setReportReason('')
    },
    onError: (err) => setReportError(err instanceof ApiError ? err.message : '提交失败'),
  })

  const query = useQuery({
    queryKey: ['listing', ref],
    queryFn: async () =>
      unwrap<ListingDetail>(await apiClient.GET('/marketplace/listings/{ref}', { params: { path: { ref: ref! } } })),
    enabled: !!ref,
  })

  if (query.isLoading) return <ListSkeleton rows={8} />
  if (query.isError) return <ErrorPanel message="资源不存在或已下架" onRetry={() => query.refetch()} />
  const listing = query.data
  if (!listing) return null

  const constraints = listing.constraints_summary
  const isAuthor = !!userId && listing.author.id === userId
  // 只有 bundle 能被 POST /runs 直接调用——agent/skill/mcp 是别的 Bundle
  // 的依赖件，本身不是一个可运行的调用目标。能不能点"运行"，看的是
  // RunBundleResolver 的解析规则：作者按所有权解析，其他人按订阅解析。
  const canRun = listing.resource_type === 'bundle' && (listing.subscribed || isAuthor)

  return (
    <div className="flex flex-col gap-space-6">
      <div className="grid grid-cols-1 gap-space-6 lg:grid-cols-[1.08fr_.92fr]">
        <div className="flex flex-col gap-space-5">
          <div>
            <div className="flex items-center gap-space-3">
              <Ref tone="blueprint">{listing.resource_type}</Ref>
              <span
                className="text-caption inline-flex items-center gap-1 text-ink-500"
                title="黑盒分发：订阅后可以运行，但看不到内部定义（V1 唯一的发布模式）"
              >
                <Lock className="size-3" aria-hidden />
                黑盒
              </span>
            </div>
            <h1 className="text-display-lg mt-space-2 text-ink-900">{listing.display_meta.display_name}</h1>
            <p className="text-body-sm mt-space-2 text-ink-500">
              @{listing.author.display_name} · v{listing.version} · {listing.subscriber_count} 人订阅 · 运行{' '}
              {listing.run_count} 次
            </p>
          </div>

          <Section title="简介">
            <p className="text-body-md text-ink-700">{listing.display_meta.description}</p>
          </Section>

          {listing.display_meta.usage && (
            <Section title="怎么用">
              <p className="text-body-md whitespace-pre-wrap text-ink-700">{listing.display_meta.usage}</p>
            </Section>
          )}

          {listing.display_meta.io_description && (
            <Section title="输入输出">
              <div className="flex flex-col gap-space-2 text-body-md text-ink-700">
                {listing.display_meta.io_description.input && <p>输入：{listing.display_meta.io_description.input}</p>}
                {listing.display_meta.io_description.outputs && listing.display_meta.io_description.outputs.length > 0 && (
                  <p>输出字段：{listing.display_meta.io_description.outputs.join('、')}</p>
                )}
              </div>
            </Section>
          )}

          {/* 这是这个页面真正的业务逻辑：一个资源发布出来到底怎么被用起来。
              bundle 有两条路——平台内直接运行，或者第三方按 Open API
              调用；非 bundle 类型说清楚"为什么这里没有运行按钮"，而不是
              让人自己猜。 */}
          {listing.resource_type === 'bundle' ? (
            <Section title="调用方式">
              <div className="flex flex-col gap-space-4">
                <p className="text-body-sm text-ink-500">
                  这个应用能被调用的方式有三种：不想注册的话直接"立即体验"，平台内的标准会话页面，或者第三方系统直接调
                  Open API。
                </p>
                <Button asChild variant="outline" className="self-start">
                  <Link to={`/chat/bundle/${encodeURIComponent(listing.listing_ref)}`} target="_blank" rel="noopener noreferrer">
                    <Sparkles className="mr-1 size-4" aria-hidden />
                    立即体验
                  </Link>
                </Button>
                {canRun ? (
                  <Button asChild className="self-start bg-gradient-cta text-white hover:opacity-90">
                    <Link to={`/runs/new?bundle=${encodeURIComponent(listing.listing_ref)}`}>
                      <Play className="mr-1 size-4" aria-hidden />
                      运行
                    </Link>
                  </Button>
                ) : (
                  <p className="text-body-sm rounded-sm bg-surface-muted px-space-4 py-space-3 text-ink-700">
                    订阅之后就能在平台内直接运行——右侧"订阅"按钮。
                  </p>
                )}
                <button
                  type="button"
                  onClick={() => setApiPanelOpen((v) => !v)}
                  className="text-body-sm flex w-fit items-center gap-1 text-blueprint hover:underline"
                >
                  Open API 调用示例
                  {apiPanelOpen ? <ChevronUp className="size-4" aria-hidden /> : <ChevronDown className="size-4" aria-hidden />}
                </button>
                {apiPanelOpen && (
                  <div className="rounded-md bg-surface-muted px-space-5 py-space-4">
                    <ApiUsagePanel listingRef={listing.listing_ref} />
                  </div>
                )}
              </div>
            </Section>
          ) : (
            <Section title="调用方式">
              <p className="text-body-sm text-ink-500">
                {listing.resource_type === 'agent'
                  ? 'Agent 不能单独运行，需要被某个 Bundle 引用、随那个 Bundle 一起运行。'
                  : listing.resource_type === 'skill'
                    ? 'Skill 是工具能力，需要被 Agent 挂载后才会被调用，不能单独运行。'
                    : 'MCP 是外部工具的接入点，需要被 Agent 挂载后才会被调用，不能单独运行。'}
              </p>
            </Section>
          )}

          <button
            type="button"
            onClick={() => setReportOpen(true)}
            className="text-caption inline-flex w-fit items-center gap-1 text-ink-500 hover:text-ink-700"
          >
            <Flag className="size-3" aria-hidden />
            举报该资源
          </button>
        </div>

        <div className="flex flex-col gap-space-5 self-start lg:sticky lg:top-space-6">
          <div className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-6">
            {isAuthor ? (
              <p className="text-body-sm text-center text-ink-500">这是你发布的资源</p>
            ) : listing.subscribed ? (
              <span className="text-label-md flex w-full items-center justify-center gap-space-2 rounded-sm border border-moss bg-moss-tint py-space-2 text-moss">
                <span aria-hidden className="size-1.5 rounded-full bg-moss" />
                已订阅
              </span>
            ) : (
              <button
                type="button"
                onClick={() => setSubscribeOpen(true)}
                className="h-11 w-full rounded-sm bg-blueprint text-body-md font-medium text-white hover:bg-blueprint/90"
              >
                订阅 v{listing.version}
              </button>
            )}
            {canRun && !isAuthor && (
              <Button asChild variant="outline" className="w-full">
                <Link to={`/runs/new?bundle=${encodeURIComponent(listing.listing_ref)}`}>
                  <Play className="mr-1 size-4" aria-hidden />
                  运行
                </Link>
              </Button>
            )}
          </div>

          {constraints && (
            <Section title="执行约束" icon={<TriangleAlert className="size-4 text-signal" aria-hidden />}>
              <div className="flex flex-col gap-space-2 text-body-sm text-ink-700">
                {constraints.max_tool_calls != null && <p>最大工具调用数：{constraints.max_tool_calls}</p>}
                {constraints.timeout_seconds != null && <p>超时：{constraints.timeout_seconds}s</p>}
                {constraints.estimated_tokens_range && <p>预计 Token：{constraints.estimated_tokens_range}</p>}
                <p className="text-caption text-ink-500">用于预估运行成本</p>
              </div>
            </Section>
          )}

          {listing.versions && listing.versions.length > 0 && (
            <Section title="版本历史">
              <div className="flex flex-col gap-space-4">
                {listing.versions.map((v) => (
                  <div key={v.version} className="border-b border-border pb-space-3 last:border-0 last:pb-0">
                    <p className="text-label-md text-ink-900">v{v.version}</p>
                    {v.changelog && <p className="text-body-sm mt-space-1 text-ink-700">{v.changelog}</p>}
                  </div>
                ))}
              </div>
            </Section>
          )}
        </div>
      </div>

      <SubscribeDialog
        listing={listing}
        open={subscribeOpen}
        onOpenChange={setSubscribeOpen}
        onSubscribed={() => {
          toast.success('已订阅')
          queryClient.invalidateQueries({ queryKey: ['listing', ref] })
        }}
      />

      <Dialog
        open={reportOpen}
        onOpenChange={(open) => {
          setReportOpen(open)
          if (!open) setReportError(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>举报该资源</DialogTitle>
          </DialogHeader>
          <Textarea
            value={reportReason}
            onChange={(e) => setReportReason(e.target.value)}
            placeholder="描述遇到的问题，例如内容不当、功能与描述严重不符……"
            className="min-h-[100px]"
          />
          {reportError && (
            <p role="alert" className="text-body-sm text-rust">
              {reportError}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setReportOpen(false)} disabled={submitReport.isPending}>
              取消
            </Button>
            <Button
              disabled={!reportReason.trim() || submitReport.isPending}
              onClick={() => {
                setReportError(null)
                submitReport.mutate()
              }}
            >
              提交举报
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
