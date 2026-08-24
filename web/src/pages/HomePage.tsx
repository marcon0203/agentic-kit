import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Figure, FigureCell, FigureRow, Ref, Section } from '@/components/common/Page'
import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type UsageSummary = components['schemas']['UsageSummary']
type RunSummary = components['schemas']['RunSummary']

/* ── The hero ─────────────────────────────────────────────────────────
   The most characteristic thing about this platform is not that it has
   dashboards — it is that a run walks across a graph and then stops, on
   purpose, to wait for a person. So the hero is that, running: stations
   light up in order, the track draws itself between them, and at the gate
   everything halts and starts breathing until you answer.

   This is the one orchestrated moment in the app. Nothing else animates on
   its own, which is what lets this read as meaning rather than decoration. */

const HERO_STATIONS = [
  { id: 'pm', label: 'product_manager', note: '整理需求' },
  { id: 'arch', label: 'architect', note: '技术方案' },
  { id: 'gate', label: 'human gate', note: '等你批准' },
  { id: 'eng', label: 'fullstack_engineer', note: '实现与自测' },
  { id: 'end', label: 'END', note: '产出交付' },
] as const

const GATE_INDEX = 2

function HeroRail() {
  // `reached` is how far the run has advanced. It stops at the gate and
  // stays there — the whole point is that the platform will not walk past a
  // decision on its own.
  const [reached, setReached] = useState(0)

  useEffect(() => {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (reduced) {
      setReached(GATE_INDEX)
      return
    }
    const timers = HERO_STATIONS.slice(0, GATE_INDEX + 1).map((_, i) =>
      window.setTimeout(() => setReached(i), 260 + i * 620),
    )
    return () => timers.forEach(window.clearTimeout)
  }, [])

  return (
    <div className="bg-blueprint-grid relative overflow-hidden rounded-lg border border-border bg-surface px-space-6 py-space-9 sm:px-space-9">
      {/* Stations size to their label; the track between them takes the
          slack, so the spacing stays even however long a node name is. */}
      <ol className="flex items-start" aria-label="一次 Bundle 运行的推进过程">
        {HERO_STATIONS.map((station, i) => {
          const arrived = i <= reached
          const halted = i === GATE_INDEX && reached >= GATE_INDEX

          return (
            <li key={station.id} className="contents">
              {/* The whole track is always drawn — you need to see where the
                  run is headed, not just where it has been. Only the
                  travelled part is inked in, and it draws itself on arrival. */}
              {i > 0 ? (
                <span
                  aria-hidden
                  className="mt-[7px] h-px min-w-space-4 flex-1 bg-border-strong"
                >
                  <span
                    className={cn(
                      'block h-px origin-left bg-blueprint transition-transform duration-500 ease-out',
                      arrived ? 'scale-x-100' : 'scale-x-0',
                    )}
                  />
                </span>
              ) : null}

              <span className="flex shrink-0 flex-col items-center gap-space-2 text-center">
                <span
                  aria-hidden
                  className={cn(
                    'size-3.5 rounded-full border-2 transition-colors duration-300',
                    halted
                      ? 'animate-gate-await border-signal bg-signal'
                      : arrived
                        ? 'border-blueprint bg-blueprint'
                        : 'border-border-strong bg-surface',
                  )}
                />
                <span className="flex flex-col items-center gap-0.5">
                  <span
                    className={cn(
                      'text-ref transition-colors duration-300',
                      halted ? 'text-signal' : arrived ? 'text-ink-900' : 'text-ink-500',
                    )}
                  >
                    {station.label}
                  </span>
                  <span
                    className={cn(
                      'text-caption transition-colors duration-300',
                      halted ? 'text-signal' : 'text-ink-500',
                    )}
                  >
                    {station.note}
                  </span>
                </span>
              </span>
            </li>
          )
        })}
      </ol>

      <p className="text-caption mt-space-8 border-t border-border pt-space-4 text-ink-500">
        运行停在 <span className="text-ref text-signal">human gate</span>
        ，直到有人批准或驳回才会继续。橙色在这个平台上只有一个含义：需要你介入。
      </p>
    </div>
  )
}

/* ── The four centres ────────────────────────────────────────────────
   Not four identical icon-in-a-square cards. Each centre gets one line
   saying what you keep there, because that is what a person is actually
   choosing between. */
const CENTRES = [
  {
    to: '/apps',
    name: '应用中心',
    holds: 'Agent 定义与 Bundle 编排',
    line: '用 DSL 描述角色与能力，拖拽连线组成协作图，从这里发起运行。',
  },
  {
    to: '/resources',
    name: '资源中心',
    holds: 'Tool · Skill · MCP · 知识库',
    line: 'Agent 能引用的一切都先在这里登记；凭证加密落库，任何响应里都不会出现。',
  },
  {
    to: '/models',
    name: '模型中心',
    holds: 'Provider 凭证',
    line: '接入 Anthropic / OpenAI / Google。凭证先验证再保存，存不进去的 key 不会等到运行时才报错。',
  },
  {
    to: '/marketplace',
    name: '应用广场',
    holds: '别人发布的能力',
    line: '订阅即用，版本锁定在订阅那一刻。作者的提示词与编排图不会随之泄露。',
  },
]

const STEPS = [
  { label: '接入资源', line: '注册 Tool / Skill / MCP / 知识库，接入模型 Provider。' },
  { label: '定义 Agent', line: '写清角色、人设、能力白名单与执行约束，按版本管理。' },
  { label: '编排 Bundle', line: '拖拽连线组图，支持条件分支、并行与 human gate。' },
  { label: '运行与审批', line: 'Chat 式时间线实时回放，关键节点停下等人。' },
]

export function HomePage() {
  const user = useAuthStore((s) => s.user)
  const openModal = useAuthStore((s) => s.openModal)

  const usageQuery = useQuery({
    queryKey: ['usage-me', 'month'],
    queryFn: async () =>
      unwrap<UsageSummary>(
        await apiClient.GET('/usage/me', { params: { query: { period: 'month' } } }),
      ),
    enabled: !!user,
  })

  const runsQuery = useQuery({
    queryKey: ['home-recent-runs'],
    queryFn: async () =>
      unwrap<{ items: RunSummary[] }>(
        await apiClient.GET('/runs', { params: { query: { sort: '-created_at', limit: 5 } } }),
      ),
    enabled: !!user,
  })

  const recentRuns = runsQuery.data?.items ?? []

  return (
    <div className="flex flex-col gap-space-11 pb-space-9">
      {/* ── Hero ───────────────────────────────────────────────── */}
      <section className="flex flex-col gap-space-8">
        <div className="flex max-w-[48ch] flex-col gap-space-4">
          <span className="text-eyebrow text-ink-500">ORCHESTRATE · RUN · APPROVE</span>
          <h1 className="text-display-xl text-ink-900">
            {user ? (
              <>
                欢迎回来，
                <br />
                {user.display_name}
              </>
            ) : (
              <>
                让多个 Agent
                <br />
                按图协作
              </>
            )}
          </h1>
          <p className="text-body-lg text-ink-700">
            把一次协作写成可复现的编排：谁先做、谁并行、哪一步必须有人点头。运行时逐步回放，出问题能定位到具体节点。
          </p>

          <div className="mt-space-3 flex flex-wrap items-center gap-space-3">
            {user ? (
              <Button asChild size="lg">
                <Link to="/apps">
                  进入应用中心
                  <ArrowRight className="size-4" aria-hidden />
                </Link>
              </Button>
            ) : (
              <Button size="lg" onClick={() => openModal('manual')}>
                创建账号
                <ArrowRight className="size-4" aria-hidden />
              </Button>
            )}
            <Button asChild variant="outline" size="lg">
              <Link to="/marketplace">浏览应用广场</Link>
            </Button>
          </div>
        </div>

        <HeroRail />
      </section>

      {/* ── 本月用量 ─────────────────────────────────────────────
          Only once something has actually run. Telling a new account it has
          spent $0.00 across 0 runs is the empty dashboard this redesign set
          out to remove — the four centres below are what they need instead. */}
      {user && (usageQuery.data?.run_count ?? 0) > 0 && (
        <Section
          title="本月用量"
          aside={
            <Link
              to="/ops"
              className="text-body-sm font-medium text-blueprint hover:underline"
            >
              查看运营中心
            </Link>
          }
        >
          <FigureRow>
            <FigureCell>
              <Figure
                value={(usageQuery.data?.total_tokens ?? 0).toLocaleString()}
                label="Token 消耗"
              />
            </FigureCell>
            <FigureCell>
              <Figure
                value={`$${(usageQuery.data?.total_cost_usd ?? 0).toFixed(2)}`}
                label="成本"
              />
            </FigureCell>
            <FigureCell>
              <Figure value={(usageQuery.data?.run_count ?? 0).toString()} label="运行次数" />
            </FigureCell>
          </FigureRow>
        </Section>
      )}

      {/* ── 最近运行 ───────────────────────────────────────────── */}
      {user && recentRuns.length > 0 && (
        <Section
          title="最近运行"
          aside={
            <Link to="/ops" className="text-body-sm font-medium text-blueprint hover:underline">
              查看全部
            </Link>
          }
        >
          <ul className="overflow-hidden rounded-lg border border-border bg-surface">
            {recentRuns.map((run) => (
              <li key={run.run_id} className="border-b border-border last:border-0">
                <Link
                  to={`/runs/${run.run_id}`}
                  className="flex items-center gap-space-4 px-space-5 py-space-3 transition-colors hover:bg-surface-muted"
                >
                  <RunStatusDot status={run.status} />
                  <span className="text-body-md min-w-0 flex-1 truncate text-ink-900">
                    {run.bundle_ref}
                  </span>
                  <Ref tone="muted">{run.run_id}</Ref>
                  <RunStatusLabel status={run.status} />
                </Link>
              </li>
            ))}
          </ul>
        </Section>
      )}

      {/* ── 四个中心 ───────────────────────────────────────────── */}
      <Section title="平台由四个中心组成">
        <ul className="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-2">
          {CENTRES.map((centre) => (
            <li key={centre.to} className="bg-surface">
              <Link
                to={centre.to}
                className="group flex h-full flex-col gap-space-2 p-space-6 transition-colors hover:bg-surface-muted"
              >
                <span className="flex items-center justify-between gap-space-3">
                  <span className="text-display-md text-ink-900">{centre.name}</span>
                  <ArrowRight
                    aria-hidden
                    className="size-4 shrink-0 text-ink-500 transition-transform group-hover:translate-x-1 group-hover:text-blueprint"
                  />
                </span>
                <span className="text-ref text-ink-500">{centre.holds}</span>
                <span className="text-body-sm mt-space-1 text-ink-700">{centre.line}</span>
              </Link>
            </li>
          ))}
        </ul>
      </Section>

      {/* ── 四步 ───────────────────────────────────────────────────
          These are numbered because they genuinely are a sequence — you
          cannot orchestrate a Bundle before defining an Agent. Drawn on the
          same rail as the hero so the ordering is shown, not just asserted
          by the digits. */}
      <Section title="第一次跑通，走这四步">
        <ol className="grid grid-cols-1 gap-space-6 sm:grid-cols-2 xl:grid-cols-4">
          {STEPS.map((step, i) => (
            <li key={step.label} className="flex flex-col gap-space-3">
              <span aria-hidden className="flex items-center gap-space-2">
                <span className="size-2 shrink-0 rounded-full bg-blueprint" />
                <span className="h-px flex-1 bg-border" />
              </span>
              <span className="flex items-baseline gap-space-2">
                <span className="text-ref text-ink-500">0{i + 1}</span>
                <span className="text-display-sm text-ink-900">{step.label}</span>
              </span>
              <span className="text-body-sm text-ink-700">{step.line}</span>
            </li>
          ))}
        </ol>
      </Section>

      {/* ── 未登录收尾 ─────────────────────────────────────────── */}
      {!user && (
        <Section title="你的资源留在你自己的空间">
          <div className="flex flex-wrap items-end justify-between gap-space-6 rounded-lg border border-border bg-surface p-space-7">
            <p className="text-body-md max-w-[56ch] text-ink-700">
              模型凭证 AES-256-GCM 加密落库，任何接口都不回显；发布到广场的资源默认黑盒，订阅者拿到的是能力本身，不是你的提示词和编排图。
            </p>
            <Button size="lg" onClick={() => openModal('manual')}>
              创建账号
            </Button>
          </div>
        </Section>
      )}
    </div>
  )
}

/* Status is carried by a dot plus a word — never colour alone. */
const RUN_STATUS = {
  running: { label: '运行中', dot: 'bg-blueprint', text: 'text-blueprint' },
  finished: { label: '已完成', dot: 'bg-moss', text: 'text-moss' },
  failed: { label: '失败', dot: 'bg-rust', text: 'text-rust' },
} as const

function RunStatusDot({ status }: { status: RunSummary['status'] }) {
  return (
    <span aria-hidden className={cn('size-2 shrink-0 rounded-full', RUN_STATUS[status].dot)} />
  )
}

function RunStatusLabel({ status }: { status: RunSummary['status'] }) {
  const meta = RUN_STATUS[status]
  return <span className={cn('text-caption w-12 shrink-0 text-right', meta.text)}>{meta.label}</span>
}
