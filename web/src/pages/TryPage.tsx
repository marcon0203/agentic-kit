import { useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Sparkles, TriangleAlert } from 'lucide-react'

import { AgentThread } from '@/components/chat/AgentThread'
import { useConversation, type StartRun } from '@/lib/runs/useConversation'
import { guestClient, getGuestAccessToken } from '@/lib/guest/guestClient'
import { useGuestSession } from '@/lib/guest/useGuestSession'
import { unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingDetail = components['schemas']['ListingDetail']
type RunSummary = components['schemas']['RunSummary']

/**
 * "立即体验"独立对话页——应用广场详情页的入口跳过来的，面向从没打开过
 * 这个平台的陌生访客。整页刻意不套 AppShell：没有平台的顶栏导航、没有
 * 侧边栏、没有"系统设置"，只有一件事能做——和这个应用聊天。鉴权也是
 * 独立的一套（lib/guest），和平台自己的登录态完全不相交。
 *
 * 数据/事件流的骨架复用 useConversation + AgentThread（黑盒事件过滤、
 * 打字机效果、人工审批卡这些已经在里面），只是换了一层更适合对外的
 * 视觉——渐变头图、居中的对话宽度，没有运行页那套执行图侧栏（那是给
 * 应用作者调试用的，访客不需要也不该看到）。
 */
export function TryPage() {
  const { listingRef } = useParams<{ listingRef: string }>()
  const { ready: guestReady, error: guestError } = useGuestSession()

  const listingQuery = useQuery({
    queryKey: ['guest-listing', listingRef],
    queryFn: async () =>
      unwrap<ListingDetail>(
        await guestClient.GET('/marketplace/listings/{ref}', { params: { path: { ref: listingRef! } } }),
      ),
    enabled: guestReady && !!listingRef,
    retry: false,
  })

  const start = useCallback<StartRun>(
    async (question, sessionID) => {
      const created = unwrap<RunSummary>(
        await guestClient.POST('/runs', {
          body: {
            bundle_ref: listingRef ?? '',
            input: { message: question },
            ...(sessionID ? { session_id: sessionID } : {}),
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      return created
    },
    [listingRef],
  )

  const listing = listingQuery.data
  const chat = useConversation({
    start,
    blocked: !guestReady || !listing,
    getAccessToken: getGuestAccessToken,
  })

  const stopRun = useCallback(async () => {
    if (!chat.activeRunID) return
    assertOk(await guestClient.POST('/runs/{id}/cancel', { params: { path: { id: chat.activeRunID } } }))
  }, [chat.activeRunID])

  if (guestError) {
    return <TryPageMessage icon={<TriangleAlert className="size-6 text-rust" aria-hidden />} title="出了点问题" hint={guestError} />
  }
  if (!guestReady || listingQuery.isLoading) {
    return <TryPageMessage icon={<Loader2 className="size-6 animate-spin text-ink-500" aria-hidden />} title="正在准备…" />
  }
  if (listingQuery.isError) {
    return (
      <TryPageMessage
        icon={<TriangleAlert className="size-6 text-rust" aria-hidden />}
        title="这个应用现在打不开"
        hint={
          listingQuery.error instanceof ApiError
            ? listingQuery.error.message
            : '它可能已经下架，或者链接不对。'
        }
      />
    )
  }
  if (!listing) return null

  return (
    <div className="fixed inset-0 flex flex-col bg-surface-page">
      <div className="bg-gradient-cta shrink-0 px-space-6 py-space-8 text-white sm:py-space-10">
        <div className="mx-auto flex max-w-[760px] items-center gap-space-4">
          <span aria-hidden className="flex size-12 shrink-0 items-center justify-center rounded-lg bg-white/15">
            <Sparkles className="size-6" />
          </span>
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-display-md truncate text-white">{listing.display_meta.display_name}</h1>
            {listing.display_meta.description && (
              <p className="text-body-sm line-clamp-2 text-white/85">{listing.display_meta.description}</p>
            )}
          </div>
        </div>
      </div>

      <div className="mx-auto flex min-h-0 w-full max-w-[760px] flex-1 flex-col">
        <AgentThread
          className="bg-transparent"
          messages={chat.messages}
          isRunning={chat.isRunning}
          onSend={chat.send}
          onCancel={stopRun}
          emptyTitle="开始体验"
          emptyHint={listing.display_meta.usage || '发一条消息试试看'}
          footerNote={
            chat.error ? (
              <p role="alert" className="text-caption text-rust">
                {chat.error}
              </p>
            ) : (
              <p className="text-caption text-ink-500">这是一次匿名体验，不需要注册或登录</p>
            )
          }
        />
      </div>
    </div>
  )
}

function TryPageMessage({ icon, title, hint }: { icon: React.ReactNode; title: string; hint?: string }) {
  return (
    <div className="fixed inset-0 flex flex-col items-center justify-center gap-space-3 bg-surface-page px-space-6 text-center">
      {icon}
      <p className="text-label-md text-ink-900">{title}</p>
      {hint && <p className="text-body-sm max-w-[420px] text-ink-500">{hint}</p>}
    </div>
  )
}
