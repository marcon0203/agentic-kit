import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown } from 'lucide-react'

import { apiClient, unwrap, assertOk, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { useAuthStore } from '@/lib/auth/store'

type RunDetail = components['schemas']['RunDetail']
import { useRunEvents } from '@/lib/runs/useRunEvents'
import { buildTimeline } from '@/lib/runs/timeline'
import { RunHeader } from '@/components/run/RunHeader'
import { RunSidebar } from '@/components/run/RunSidebar'
import { UserBubble, AgentBubble } from '@/components/run/ChatBubble'
import { GateCard } from '@/components/run/GateCard'
import { PluginRenderCard } from '@/components/run/PluginRenderCard'
import type { PlatformStatus } from '@/components/run/StatusChip'

export function RunPage() {
  const { runId } = useParams<{ runId: string }>()
  const location = useLocation()
  const initialInput = (location.state as { inputText?: string } | null)?.inputText
  const userId = useAuthStore((s) => s.user?.id)
  const queryClient = useQueryClient()

  const { events, status: streamStatus, reconnect } = useRunEvents(runId)
  const timeline = useMemo(() => buildTimeline(events), [events])

  const runQuery = useQuery({
    queryKey: ['run', runId],
    queryFn: async () => unwrap<RunDetail>(await apiClient.GET('/runs/{id}', { params: { path: { id: runId! } } })),
    enabled: !!runId,
    refetchInterval: streamStatus === 'open' ? 4000 : false,
  })

  // No shared_state.updated event exists yet (see run_engine.go's
  // finishRun/persistEvent) — refetch the run detail whenever the stream
  // reaches a terminal state so the sidebar's shared_state panel and
  // final usage numbers are current, instead of leaving the last poll.
  useEffect(() => {
    if (streamStatus === 'closed') {
      queryClient.invalidateQueries({ queryKey: ['run', runId] })
    }
  }, [streamStatus, runId, queryClient])

  const run = runQuery.data

  // Auto-scroll to bottom on new content, but pause once the user scrolls
  // up — spec-14: show a "new message" pill instead of yanking them back.
  const scrollRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const [hasNewMessage, setHasNewMessage] = useState(false)

  useEffect(() => {
    if (autoScroll) {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
      setHasNewMessage(false)
    } else {
      setHasNewMessage(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [events.length])

  function handleScroll() {
    const el = scrollRef.current
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    setAutoScroll(nearBottom)
    if (nearBottom) setHasNewMessage(false)
  }

  function scrollToBottom() {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
    setAutoScroll(true)
    setHasNewMessage(false)
  }

  const [actionError, setActionError] = useState<string | null>(null)

  async function resolveGate(node: string, approved: boolean) {
    if (!runId) return
    setActionError(null)
    try {
      assertOk(await apiClient.POST('/runs/{id}/gate', { params: { path: { id: runId } }, body: { node, approved } }))
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试')
      throw err
    }
  }

  async function stopRun() {
    if (!runId) return
    setActionError(null)
    try {
      assertOk(await apiClient.POST('/runs/{id}/cancel', { params: { path: { id: runId } } }))
      queryClient.invalidateQueries({ queryKey: ['run', runId] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '停止失败，请稍后重试')
      throw err
    }
  }

  if (!runId) return null

  const isBlackbox = run ? !run.is_owner : false
  // V1's only valid approver is the run's own triggered_by user (spec-11)
  // — the frontend has no other way to know that than "I created this
  // run's session", which for the owner/subscriber both means: I'm the
  // one who is signed in and looking at a run of my own bundle_ref that
  // I triggered. Since RunSummary doesn't expose triggered_by, treat
  // "is_owner" as the proxy: only a run's own initiator can be its
  // approver in V1, and that's always the Bundle owner running their own
  // Bundle directly (a subscriber never gates on someone else's bundle
  // by this rule) — a real per-run triggered_by check happens
  // server-side (403 + 50004) regardless of what this button shows.
  const canApprove = !!userId && !!run?.is_owner

  const runStatus: PlatformStatus =
    run?.status === 'failed' ? 'failed' : run?.status === 'finished' ? 'done' : timeline.runStatus === 'failed' ? 'failed' : 'running'

  return (
    <div className="grid grid-cols-1 gap-space-6 lg:grid-cols-[1.08fr_.92fr]">
      <div className="flex flex-col">
        <RunHeader
          runId={runId}
          status={runStatus}
          streamStatus={streamStatus}
          totalTokens={run?.usage?.total_tokens ?? 0}
          costUsd={run?.usage?.cost_usd ?? 0}
          onReconnect={reconnect}
          onStop={stopRun}
        />

        {actionError && (
          <p role="alert" className="text-body-sm mb-space-4 text-rust">
            {actionError}
          </p>
        )}

        <div className="relative flex-1 rounded-lg border border-border bg-surface p-space-6">
          <div ref={scrollRef} onScroll={handleScroll} className="flex max-h-[65vh] flex-col gap-space-5 overflow-y-auto">
            {initialInput && <UserBubble text={initialInput} />}

            {timeline.entries.length === 0 && (
              <p className="text-body-sm flex items-center gap-space-2 text-ink-500">
                <span aria-hidden className="size-2 animate-pulse rounded-full bg-blueprint" />
                正在启动，第一个节点马上开始…
              </p>
            )}

            {timeline.entries.map((entry) => {
              if (entry.kind === 'bubble-group') {
                return (
                  <div key={entry.key} className="flex flex-col gap-space-4 md:flex-row">
                    {entry.nodes.map((node) => (
                      <AgentBubble key={node} node={node} bubble={timeline.bubbles[node]} isBlackbox={isBlackbox} />
                    ))}
                  </div>
                )
              }
              if (entry.kind === 'gate') {
                return <GateCard key={entry.key} gate={entry} canApprove={canApprove} onResolve={resolveGate} />
              }
              if (entry.kind === 'render') {
                return <PluginRenderCard key={entry.key} entry={entry} />
              }
              return (
                <div
                  key={entry.key}
                  className={
                    'text-body-sm rounded-sm border px-space-4 py-space-3 ' +
                    (entry.tone === 'error'
                      ? 'border-rust bg-rust-tint text-rust'
                      : entry.tone === 'success'
                        ? 'border-moss bg-moss-tint text-moss'
                        : 'border-border text-ink-700')
                  }
                >
                  {entry.text}
                </div>
              )
            })}
          </div>

          {hasNewMessage && !autoScroll && (
            <button
              type="button"
              onClick={scrollToBottom}
              className="absolute bottom-space-4 right-space-4 flex items-center gap-1 rounded-full bg-blueprint px-space-4 py-space-2 text-body-sm text-white shadow-md"
            >
              有新消息 <ArrowDown className="size-3.5" aria-hidden />
            </button>
          )}
        </div>
      </div>

      <RunSidebar
        bubbles={timeline.bubbles}
        events={events}
        sharedState={(run?.shared_state as Record<string, unknown>) ?? {}}
        isBlackbox={isBlackbox}
        totalTokens={run?.usage?.total_tokens ?? 0}
        costUsd={run?.usage?.cost_usd ?? 0}
        durationSeconds={run?.usage?.duration_seconds ?? 0}
      />
    </div>
  )
}
