import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Menu, Plus, Search, Sparkles, Trash2, TriangleAlert } from 'lucide-react'

import { AgentThread } from '@/components/chat/AgentThread'
import { useConversation, type StartRun } from '@/lib/runs/useConversation'
import { useConversationList } from '@/lib/runs/useConversationList'
import { replayFinishedTurns } from '@/lib/runs/replaySession'
import type { ChatTurn } from '@/lib/runs/threadMessages'
import { guestClient, getGuestAccessToken } from '@/lib/guest/guestClient'
import { useGuestSession } from '@/lib/guest/useGuestSession'
import { unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ListingDetail = components['schemas']['ListingDetail']
type RunSummary = components['schemas']['RunSummary']

const HIDDEN_KEY_PREFIX = 'agentic-kit-guest-hidden-conversations:'

function readHidden(bundleId: string): Set<string> {
  try {
    const raw = sessionStorage.getItem(HIDDEN_KEY_PREFIX + bundleId)
    return raw ? new Set(JSON.parse(raw)) : new Set()
  } catch {
    return new Set()
  }
}

function writeHidden(bundleId: string, hidden: Set<string>) {
  try {
    sessionStorage.setItem(HIDDEN_KEY_PREFIX + bundleId, JSON.stringify([...hidden]))
  } catch {
    // sessionStorage 不可用时"删除"就只在这次渲染里生效，不是致命问题。
  }
}

/**
 * "立即体验"独立对话页——应用广场详情页的入口跳过来的，面向从没打开过
 * 这个平台的陌生访客。整页刻意不套 AppShell：没有平台的顶栏导航、没有
 * 侧边栏、没有"系统设置"。鉴权也是独立的一套（lib/guest），和平台自己的
 * 登录态完全不相交。
 *
 * 路由按 Bundle 寻址（/chat/bundle/:bundleId）而不是按某一次运行——一个
 * 访客可以在同一个 Bundle 下面开好几段对话，左侧栏就是这些对话的列表。
 * 点开一段历史对话会把它重放出来（复用运行页刷新重建对话用的同一套
 * replayFinishedTurns），不是简单地清空重来。
 *
 * 消息渲染仍然是 useConversation + AgentThread（黑盒事件过滤、打字机效
 * 果、人工审批卡这些已经在里面），这一层只负责外面这圈"聊天应用"的骨架
 * ——侧栏、顶栏、响应式抽屉。
 */
export function TryPage() {
  const { bundleId } = useParams<{ bundleId: string }>()
  const { ready: guestReady, error: guestError } = useGuestSession()

  const listingQuery = useQuery({
    queryKey: ['guest-listing', bundleId],
    queryFn: async () =>
      unwrap<ListingDetail>(await guestClient.GET('/marketplace/listings/{ref}', { params: { path: { ref: bundleId! } } })),
    enabled: guestReady && !!bundleId,
    retry: false,
  })
  const listing = listingQuery.data

  const { conversations, refresh: refreshConversations } = useConversationList(guestClient, bundleId, guestReady)

  const [hidden, setHidden] = useState<Set<string>>(() => (bundleId ? readHidden(bundleId) : new Set()))
  useEffect(() => {
    if (bundleId) setHidden(readHidden(bundleId))
  }, [bundleId])
  const visibleConversations = useMemo(
    () => conversations.filter((c) => !hidden.has(c.session_id)),
    [conversations, hidden],
  )

  const [query, setQuery] = useState('')
  const filteredConversations = useMemo(() => {
    if (!query.trim()) return visibleConversations
    const q = query.trim().toLowerCase()
    return visibleConversations.filter((c) => c.title.toLowerCase().includes(q))
  }, [visibleConversations, query])

  // undefined = 还没开始的新对话（草稿）。切换到一段既有对话或点"新建对
  // 话"都是刻意的重置动作，靠 resetNonce 强制下面那棵子树重新挂载，重放
  // 逻辑再跑一遍；同一段对话里"发第一条消息后拿到 session_id"这种自然
  // 演进则完全不触碰这两个 state，子树不会被重新挂载。
  const [viewingSessionId, setViewingSessionId] = useState<string | undefined>(undefined)
  const [resetNonce, setResetNonce] = useState(0)
  const startNewConversation = useCallback(() => {
    setViewingSessionId(undefined)
    setResetNonce((n) => n + 1)
  }, [])
  const openConversation = useCallback((sessionId: string) => {
    setViewingSessionId(sessionId)
    setResetNonce((n) => n + 1)
  }, [])
  const hideConversation = useCallback(
    (sessionId: string, e: React.MouseEvent) => {
      e.stopPropagation()
      if (!bundleId) return
      const next = new Set(hidden)
      next.add(sessionId)
      setHidden(next)
      writeHidden(bundleId, next)
      if (viewingSessionId === sessionId) startNewConversation()
    },
    [bundleId, hidden, viewingSessionId, startNewConversation],
  )

  const [sidebarOpen, setSidebarOpen] = useState(false)

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
        hint={listingQuery.error instanceof ApiError ? listingQuery.error.message : '它可能已经下架，或者链接不对。'}
      />
    )
  }
  if (!listing || !bundleId) return null

  const activeTitle = viewingSessionId
    ? (conversations.find((c) => c.session_id === viewingSessionId)?.title ?? '对话')
    : '新的对话'

  return (
    <div
      className="fixed inset-0 flex bg-surface-page"
      style={{
        backgroundImage:
          'radial-gradient(120% 90% at 15% 0%, var(--color-blueprint-tint), transparent 60%), radial-gradient(100% 80% at 85% 10%, var(--color-violet-tint), transparent 55%)',
      }}
    >
      {sidebarOpen && (
        <button
          type="button"
          aria-label="关闭对话列表"
          className="fixed inset-0 z-10 bg-black/30 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      <aside
        className={`fixed inset-y-0 left-0 z-20 flex w-[260px] shrink-0 flex-col gap-space-2 border-r border-border bg-surface p-space-3 transition-transform duration-200 lg:static lg:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex items-center gap-space-2 px-space-2 py-space-2">
          <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-sm bg-blueprint-tint text-blueprint">
            <Sparkles className="size-5" />
          </span>
          <p className="text-label-md truncate text-ink-900">{listing.display_meta.display_name}</p>
        </div>

        <button
          type="button"
          onClick={() => {
            startNewConversation()
            setSidebarOpen(false)
          }}
          className="flex h-10 items-center justify-center gap-space-2 rounded-sm bg-gradient-cta text-body-sm font-semibold text-white shadow-[0_6px_16px_rgb(124_92_252_/_0.3)] hover:opacity-90"
        >
          <Plus className="size-4" aria-hidden />
          新建对话
        </button>

        <label className="flex h-9 items-center gap-space-2 rounded-sm border border-border px-space-3 text-ink-500 focus-within:border-blueprint">
          <Search className="size-4 shrink-0" aria-hidden />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            type="text"
            placeholder="搜索对话"
            aria-label="搜索对话"
            className="text-body-sm min-w-0 flex-1 bg-transparent text-ink-900 outline-none placeholder:text-ink-500"
          />
        </label>

        <nav className="flex min-h-0 flex-1 flex-col gap-space-1 overflow-y-auto" aria-label="对话列表">
          {filteredConversations.length === 0 && (
            <p className="text-body-sm px-space-2 py-space-3 text-ink-500">
              {conversations.length === 0 ? '还没有对话，从下方输入框开始吧。' : `没有匹配"${query}"的对话`}
            </p>
          )}
          {filteredConversations.map((c) => (
            <button
              key={c.session_id}
              type="button"
              aria-current={viewingSessionId === c.session_id ? 'true' : undefined}
              onClick={() => {
                openConversation(c.session_id)
                setSidebarOpen(false)
              }}
              className="group flex h-10 items-center gap-space-2 rounded-full px-space-3 text-left text-body-sm text-ink-700 hover:bg-blueprint-tint hover:text-ink-900 aria-[current=true]:bg-gradient-cta aria-[current=true]:text-white aria-[current=true]:shadow-[0_6px_16px_rgb(124_92_252_/_0.3)]"
            >
              <span className="min-w-0 flex-1 truncate">{c.title || '新的对话'}</span>
              <span
                role="button"
                tabIndex={0}
                aria-label="删除对话"
                onClick={(e) => hideConversation(c.session_id, e)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    hideConversation(c.session_id, e as unknown as React.MouseEvent)
                  }
                }}
                className="hidden shrink-0 rounded-xs p-1 opacity-70 hover:bg-black/10 hover:opacity-100 group-hover:flex"
              >
                <Trash2 className="size-3.5" aria-hidden />
              </span>
            </button>
          ))}
        </nav>

        <p className="text-caption border-t border-border px-space-2 pt-space-3 text-ink-500">
          这是一次匿名体验，不需要注册或登录
        </p>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-space-3 border-b border-border bg-surface/80 px-space-4 backdrop-blur">
          <button
            type="button"
            onClick={() => setSidebarOpen(true)}
            aria-label="打开对话列表"
            className="flex size-9 items-center justify-center rounded-sm text-ink-700 hover:bg-surface-muted lg:hidden"
          >
            <Menu className="size-5" aria-hidden />
          </button>
          <p className="text-label-md min-w-0 flex-1 truncate text-ink-900">{activeTitle}</p>
          <span className="text-caption shrink-0 rounded-full border border-border px-space-3 py-1 text-ink-500">
            {listing.display_meta.display_name} · v{listing.version}
          </span>
        </header>

        <ChatBundleConversation
          key={resetNonce}
          bundleId={bundleId}
          sessionId={viewingSessionId}
          listing={listing}
          onConversationStarted={(sid) => setViewingSessionId(sid)}
          onSettled={refreshConversations}
        />
      </div>
    </div>
  )
}

/**
 * 一段对话的实际渲染——按 resetNonce 重新挂载，内部状态（useConversation
 * 攒的 turns/sessionID）不会跨"切换到另一段对话"这个动作残留。
 */
function ChatBundleConversation({
  bundleId,
  sessionId,
  listing,
  onConversationStarted,
  onSettled,
}: {
  bundleId: string
  sessionId: string | undefined
  listing: ListingDetail
  onConversationStarted: (sessionId: string) => void
  onSettled: () => void
}) {
  const sessionRunsQuery = useQuery({
    queryKey: ['guest-session-runs', sessionId],
    queryFn: async () =>
      unwrap<RunSummary[]>(await guestClient.GET('/sessions/{id}/runs', { params: { path: { id: sessionId! } } })),
    enabled: !!sessionId,
  })

  const [history, setHistory] = useState<ChatTurn[] | undefined>(sessionId ? undefined : [])
  useEffect(() => {
    if (!sessionId) {
      setHistory([])
      return
    }
    const rows = sessionRunsQuery.data
    if (!rows) return
    const liveRunID = rows.find((r) => r.status === 'running')?.run_id
    const controller = new AbortController()
    replayFinishedTurns(
      rows.map((r) => r.run_id),
      liveRunID,
      controller.signal,
      getGuestAccessToken,
    )
      .then(setHistory)
      .catch(() => setHistory([]))
    return () => controller.abort()
  }, [sessionId, sessionRunsQuery.data])

  const liveRunID = sessionId ? sessionRunsQuery.data?.find((r) => r.status === 'running')?.run_id : undefined

  const start = useCallback<StartRun>(
    async (question, sid) => {
      const created = unwrap<RunSummary>(
        await guestClient.POST('/runs', {
          body: {
            bundle_ref: bundleId,
            input: { message: question },
            ...(sid ? { session_id: sid } : {}),
          },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      return created
    },
    [bundleId],
  )

  const chat = useConversation({
    start,
    initialSessionID: sessionId,
    initialTurns: history,
    initialActiveRunID: history !== undefined ? liveRunID : undefined,
    blocked: history === undefined,
    getAccessToken: getGuestAccessToken,
  })

  useEffect(() => {
    if (chat.sessionID && chat.sessionID !== sessionId) onConversationStarted(chat.sessionID)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chat.sessionID])

  // 跑完一轮（isRunning 由 true 变 false）就通知外层刷新侧栏——新对话的标
  // 题、排序都要等第一轮跑完才有数据。ref 存前一次的值，不放进依赖数组
  // 是刻意的：这不是要响应 onSettled 的变化，只是读一次"上一次渲染时的
  // isRunning"。
  const wasRunningRef = useRef(chat.isRunning)
  useEffect(() => {
    if (wasRunningRef.current && !chat.isRunning) onSettled()
    wasRunningRef.current = chat.isRunning
  }, [chat.isRunning, onSettled])

  const stopRun = useCallback(async () => {
    if (!chat.activeRunID) return
    assertOk(await guestClient.POST('/runs/{id}/cancel', { params: { path: { id: chat.activeRunID } } }))
  }, [chat.activeRunID])

  return (
    <div className="min-h-0 flex-1">
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
          ) : undefined
        }
      />
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
