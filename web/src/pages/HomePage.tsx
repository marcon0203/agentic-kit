import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  ArrowRight,
  Bot,
  ChevronDown,
  Cpu,
  Database,
  GitBranch,
  ShieldCheck,
  Store,
  Zap,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Figure, FigureCell, FigureRow, Ref, Section } from '@/components/common/Page'
import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import { cn } from '@/lib/utils'
import type { components } from '@/lib/api/schema'

type UsageSummary = components['schemas']['UsageSummary']
type RunSummary = components['schemas']['RunSummary']

/* ── Hero pipeline stations ───────────────────────────────────────────
   These are the same five stations the current hero rail used, redrawn
   as a pipeline illustration so the visitor sees both the path and the
   deliberate stop at the gate. */

const HERO_STATIONS = [
  { id: 'pm', label: 'product_manager', note: '整理需求' },
  { id: 'arch', label: 'architect', note: '技术方案' },
  { id: 'gate', label: 'human gate', note: '等你批准' },
  { id: 'eng', label: 'fullstack_engineer', note: '实现与自测' },
  { id: 'end', label: 'END', note: '产出交付' },
] as const

const GATE_INDEX = 2

/* ── Proof stats ────────────────────────────────────────────────────── */

const PROOF = [
  { value: '2', label: '能力中心' },
  { value: '4', label: '步跑通' },
  { value: '1', label: 'Human Gate 审批' },
]

/* ── The two centres ─────────────────────────────────────────────────
   Each centre gets an icon, a colour, and one line saying what you keep
   there, because that is what a person is actually choosing between.
   应用广场是发布市场、自己的 Agent/Bundle 管理台，也是资源登记处——三者
   互为同一条主线上的环节，拆成几个中心反而让人来回切，所以合成一个。 */

const CENTRES = [
  {
    to: '/apps',
    name: '应用广场',
    holds: 'Agent · Bundle · 资源登记 · 发布市场',
    line: '自己建的 Agent 与 Bundle 在这里编排、发起运行；Tool/Skill/MCP/知识库/记忆库在这里登记；别人发布的能力也在这里订阅即用。',
    icon: Store,
    tone: 'blueprint' as const,
  },
  {
    to: '/models',
    name: '模型广场',
    holds: 'Provider 凭证',
    line: '接入 Anthropic / OpenAI / Google。凭证先验证再保存，存不进去的 key 不会等到运行时才报错。',
    icon: Cpu,
    tone: 'moss' as const,
  },
]

/* ── The four steps ─────────────────────────────────────────────────── */

const STEPS = [
  {
    label: '接入资源',
    line: '注册 Tool / Skill / MCP / 知识库，接入模型 Provider。',
    icon: Database,
  },
  {
    label: '定义 Agent',
    line: '写清角色、人设、能力白名单与执行约束，按版本管理。',
    icon: Bot,
  },
  {
    label: '编排 Bundle',
    line: '拖拽连线组图，支持条件分支、并行与 human gate。',
    icon: GitBranch,
  },
  {
    label: '运行与审批',
    line: 'Chat 式时间线实时回放，关键节点停下等人。',
    icon: ShieldCheck,
  },
]

/* ── FAQ ────────────────────────────────────────────────────────────── */

const FAQS = [
  {
    q: 'Agentic Kit 是什么？',
    a: '一个 Agent 编排与运行平台。你可以把多个 Agent 按协作图组织起来，规定谁先跑、谁并行、哪一步必须有人点头，然后让运行按图推进、并在关键节点停下来等人审批。',
  },
  {
    q: 'Human Gate 是什么？',
    a: 'Human Gate 是运行流中的一个强制暂停点。Agent 不会自己越过需要人类判断的节点；平台会停下来，等有人批准或驳回后才继续。',
  },
  {
    q: '两个中心分别做什么？',
    a: '应用广场负责 Agent 与 Bundle 的编排管理，也是资源登记处（Tool、Skill、MCP、知识库、记忆库）和发布市场——自己建的和别人发布的都在这里，互不打扰；模型广场管理 LLM Provider 与凭证。',
  },
  {
    q: '运行出错了怎么定位？',
    a: '每一次运行都有时间线回放，可以按节点查看输入、输出与状态。失败节点会标红，并保留完整上下文供你重试或打回。',
  },
  {
    q: '凭证和数据安全吗？',
    a: '模型凭证 AES-256-GCM 加密落库，任何接口都不回显；发布到广场的资源默认黑盒，订阅者拿到的是能力本身，不是你的提示词和编排图。',
  },
  {
    q: '第一次使用应该从哪里开始？',
    a: '建议按「接入资源 → 定义 Agent → 编排 Bundle → 运行与审批」四步走。即使只有单个 Agent，也可以先跑起来熟悉 Human Gate 的审批流程。',
  },
]

/* ── Hero illustration ───────────────────────────────────────────────
   A self-contained SVG that shows the pipeline and the gate. The moving
   dot and the pulsing gate respect prefers-reduced-motion. */

function HeroIllustration() {
  const [reduced, setReduced] = useState(false)

  useEffect(() => {
    setReduced(window.matchMedia('(prefers-reduced-motion: reduce)').matches)
  }, [])

  return (
    <div className="relative overflow-hidden rounded-xl border border-border bg-surface p-space-5 shadow-status-sm">
      <svg viewBox="0 0 480 320" className="w-full" aria-hidden="true">
        <defs>
          <linearGradient id="bp-violet" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="#2563eb" />
            <stop offset="100%" stopColor="#7c3aed" />
          </linearGradient>
        </defs>

        {/* Card ground */}
        <rect x="0" y="0" width="480" height="320" rx="14" fill="#f8fafc" />

        {/* Header */}
        <text x="24" y="38" fontSize="12" fontWeight="700" fill="#0f172a">
          DELIVERY PIPELINE
        </text>

        {/* Pipeline nodes */}
        <g transform="translate(48, 80)">
          {HERO_STATIONS.map((station, i) => {
            const cx = i * 96
            const isGate = i === GATE_INDEX
            const reached = i <= GATE_INDEX
            return (
              <g key={station.id}>
                {i > 0 && (
                  <line
                    x1={(i - 1) * 96}
                    y1="0"
                    x2={cx}
                    y2="0"
                    stroke={reached ? 'url(#bp-violet)' : '#e2e8f0'}
                    strokeWidth="3"
                    strokeLinecap="round"
                  />
                )}
                <circle
                  cx={cx}
                  cy="0"
                  r={isGate ? 10 : 8}
                  fill={reached ? (isGate ? '#f59e0b' : '#2563eb') : '#e2e8f0'}
                  stroke="#fff"
                  strokeWidth="3"
                >
                  {!reduced && isGate && (
                    <animate
                      attributeName="r"
                      values="10;13;10"
                      dur="2.4s"
                      repeatCount="indefinite"
                    />
                  )}
                </circle>
                <text
                  x={cx}
                  y="32"
                  textAnchor="middle"
                  fontSize="11"
                  fontWeight="600"
                  fill={reached ? '#0f172a' : '#94a3b8'}
                >
                  {station.note}
                </text>
              </g>
            )
          })}

          {/* Travelling dot */}
          {!reduced && (
            <circle r="5" fill="#2563eb">
              <animateMotion
                dur="3s"
                repeatCount="indefinite"
                path="M 0 0 L 96 0 L 192 0"
              />
            </circle>
          )}
        </g>

        {/* Terminal panel */}
        <rect x="24" y="160" width="432" height="136" rx="14" fill="#0b1220" />
        <g fontFamily="var(--font-mono)" fontSize="11">
          <text x="44" y="196" fill="#6ee7b7">
            $ agentic-kit run --bundle demo
          </text>
          <text x="44" y="222" fill="#93c5fd">
            ✓ 需求解析完成
          </text>
          <text x="44" y="248" fill="#93c5fd">
            ✓ 技术方案已生成
          </text>
          <text x="44" y="274" fill="#f59e0b">
            ▶ 停在 human gate，等待审批…
            {!reduced && (
              <animate attributeName="opacity" values="1;.3;1" dur="1.4s" repeatCount="indefinite" />
            )}
          </text>
        </g>
      </svg>
    </div>
  )
}

/* ── Hero section ───────────────────────────────────────────────────── */

function HeroSection() {
  const user = useAuthStore((s) => s.user)
  const openModal = useAuthStore((s) => s.openModal)

  return (
    <section className="relative overflow-hidden rounded-2xl bg-surface px-space-6 py-space-10 md:px-space-10 md:py-space-12">
      {/* Soft radial glow in the corner, like the reference hero. */}
      <div
        aria-hidden
        className="pointer-events-none absolute -right-24 -top-24 size-[500px] rounded-full bg-blueprint/5 blur-3xl"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -bottom-32 -left-32 size-[420px] rounded-full bg-violet/5 blur-3xl"
      />

      <div className="relative grid gap-space-10 md:grid-cols-2 md:items-center">
        <div className="flex flex-col gap-space-5">
          <span className="inline-flex w-max items-center gap-space-2 rounded-full bg-blueprint-tint px-space-3 py-1.5 text-eyebrow text-blueprint">
            <Zap className="size-3.5" aria-hidden />
            AGENTIC KIT · ORCHESTRATE · RUN · APPROVE
          </span>

          <h1 className="text-display-xl text-ink-900">
            {user ? (
              <>
                欢迎回来，
                <br />
                <span className="text-gradient">{user.display_name}</span>
              </>
            ) : (
              <>
                让多个 Agent
                <br />
                <span className="text-gradient">按图协作</span>
              </>
            )}
          </h1>

          <p className="text-body-lg max-w-[54ch] text-ink-700">
            把一次协作写成可复现的编排：谁先做、谁并行、哪一步必须有人点头。运行时逐步回放，出问题能定位到具体节点。
          </p>

          <div className="flex flex-wrap items-center gap-space-3">
            {user ? (
              <Button asChild size="lg" className="bg-gradient-cta text-white hover:opacity-90">
                <Link to="/apps/bundles">
                  进入应用管理
                  <ArrowRight className="size-4" aria-hidden />
                </Link>
              </Button>
            ) : (
              <Button size="lg" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => openModal('manual')}>
                创建账号
                <ArrowRight className="size-4" aria-hidden />
              </Button>
            )}
            <Button asChild variant="outline" size="lg">
              <Link to="/apps">浏览应用广场</Link>
            </Button>
          </div>

          <div className="mt-space-2 flex flex-wrap gap-space-7">
            {PROOF.map((item) => (
              <div key={item.label} className="flex flex-col gap-space-1">
                <span className="text-figure text-ink-900">{item.value}</span>
                <span className="text-caption text-ink-500">{item.label}</span>
              </div>
            ))}
          </div>
        </div>

        <HeroIllustration />
      </div>
    </section>
  )
}

/* ── Monthly usage ──────────────────────────────────────────────────── */

function UsageSection({ data }: { data: UsageSummary }) {
  return (
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
      <FigureRow className="rounded-xl shadow-status-sm">
        <FigureCell>
          <Figure value={(data.total_tokens ?? 0).toLocaleString()} label="Token 消耗" />
        </FigureCell>
        <FigureCell>
          <Figure value={`$${(data.total_cost_usd ?? 0).toFixed(2)}`} label="成本" />
        </FigureCell>
        <FigureCell>
          <Figure value={(data.run_count ?? 0).toString()} label="运行次数" />
        </FigureCell>
      </FigureRow>
    </Section>
  )
}

/* ── Recent runs ────────────────────────────────────────────────────── */

function RecentRunsSection({ runs }: { runs: RunSummary[] }) {
  return (
    <Section
      title="最近运行"
      aside={
        <Link to="/ops" className="text-body-sm font-medium text-blueprint hover:underline">
          查看全部
        </Link>
      }
    >
      <ul className="overflow-hidden rounded-xl border border-border bg-surface shadow-status-sm">
        {runs.map((run) => (
          <li key={run.run_id} className="border-b border-border last:border-0">
            <Link
              to={`/runs/${run.run_id}`}
              className="flex items-center gap-space-4 px-space-5 py-space-3 transition-colors hover:bg-surface-muted"
            >
              <RunStatusDot status={run.status} />
              <span className="text-body-md min-w-0 flex-1 truncate text-ink-900">{run.bundle_ref}</span>
              <Ref tone="muted">{run.run_id}</Ref>
              <RunStatusLabel status={run.status} />
            </Link>
          </li>
        ))}
      </ul>
    </Section>
  )
}

/* ── Centres grid ───────────────────────────────────────────────────── */

const TONE_STYLES = {
  blueprint: 'bg-blueprint-tint text-blueprint',
  violet: 'bg-violet-tint text-violet',
  moss: 'bg-moss-tint text-moss',
  signal: 'bg-signal-tint text-signal',
}

function CentresSection() {
  return (
    <Section title="平台由两个中心组成">
      <ul className="grid grid-cols-1 gap-space-4 md:grid-cols-2">
        {CENTRES.map((centre) => {
          const Icon = centre.icon
          return (
            <li key={centre.to}>
              <Link
                to={centre.to}
                className="group flex h-full flex-col gap-space-4 rounded-xl border border-border bg-surface p-space-5 transition-all hover:border-blueprint-edge hover:shadow-status-sm"
              >
                <span className="flex items-center justify-between">
                  <span
                    className={cn(
                      'flex size-11 items-center justify-center rounded-xl',
                      TONE_STYLES[centre.tone],
                    )}
                  >
                    <Icon className="size-5" aria-hidden />
                  </span>
                  <ArrowRight
                    aria-hidden
                    className="size-4 shrink-0 text-ink-500 transition-transform group-hover:translate-x-1 group-hover:text-blueprint"
                  />
                </span>
                <span className="flex flex-col gap-space-1">
                  <span className="text-display-sm text-ink-900">{centre.name}</span>
                  <span className="text-ref text-ink-500">{centre.holds}</span>
                </span>
                <span className="text-body-sm mt-auto text-ink-700">{centre.line}</span>
              </Link>
            </li>
          )
        })}
      </ul>
    </Section>
  )
}

/* ── Steps section ──────────────────────────────────────────────────── */

function StepsSection() {
  return (
    <Section title="第一次跑通，走这四步">
      <ol className="relative grid grid-cols-1 gap-space-6 md:grid-cols-2 xl:grid-cols-4">
        {/* Horizontal connecting rail behind the cards. */}
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-0 top-[22px] hidden h-px bg-border xl:block"
        />

        {STEPS.map((step, i) => {
          const Icon = step.icon
          return (
            <li key={step.label} className="relative flex flex-col gap-space-3">
              <span aria-hidden className="flex items-center gap-space-2">
                <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-blueprint-tint text-blueprint">
                  <Icon className="size-5" aria-hidden />
                </span>
                <span className="h-px flex-1 bg-border xl:hidden" />
              </span>
              <span className="flex items-baseline gap-space-2">
                <span className="text-ref text-ink-500">0{i + 1}</span>
                <span className="text-display-sm text-ink-900">{step.label}</span>
              </span>
              <span className="text-body-sm text-ink-700">{step.line}</span>
            </li>
          )
        })}
      </ol>
    </Section>
  )
}

/* ── FAQ section ────────────────────────────────────────────────────── */

function FAQSection() {
  return (
    <Section title="常见问题">
      <div className="flex flex-col gap-space-3">
        {FAQS.map((faq) => (
          <details
            key={faq.q}
            className="group rounded-xl border border-border bg-surface transition-colors open:border-blueprint-edge open:bg-blueprint-tint/30"
          >
            <summary className="flex cursor-pointer list-none items-center justify-between gap-space-4 px-space-5 py-space-4 text-body-md font-medium text-ink-900">
              {faq.q}
              <ChevronDown
                aria-hidden
                className="size-4 shrink-0 text-ink-500 transition-transform group-open:rotate-180"
              />
            </summary>
            <div className="px-space-5 pb-space-4 text-body-sm text-ink-700">{faq.a}</div>
          </details>
        ))}
      </div>
    </Section>
  )
}

/* ── Bottom CTA banner ──────────────────────────────────────────────── */

function CTABanner() {
  const openModal = useAuthStore((s) => s.openModal)

  return (
    <section className="-mx-space-6 bg-surface-page px-space-6 py-space-10">
      <div className="mx-auto max-w-container-app">
        <div className="flex flex-col items-start justify-between gap-space-6 rounded-2xl bg-gradient-cta px-space-8 py-space-9 text-white md:flex-row md:items-center">
          <div className="flex flex-col gap-space-2">
            <h2 className="text-display-lg text-white">让 Agent 按图协作，从这一步开始</h2>
            <p className="text-body-md text-white/85 max-w-[52ch]">
              创建账号，接入第一个模型 Provider，然后定义你的第一个 Agent。几分钟内就能发起一次带 Human Gate 的运行。
            </p>
          </div>
          <Button
            size="lg"
            className="shrink-0 bg-white text-blueprint hover:bg-white/90"
            onClick={() => openModal('manual')}
          >
            创建账号
            <ArrowRight className="size-4" aria-hidden />
          </Button>
        </div>
      </div>
    </section>
  )
}

/* ── Page ───────────────────────────────────────────────────────────── */

export function HomePage() {
  const user = useAuthStore((s) => s.user)

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
  const usage = usageQuery.data

  return (
    <div className="flex flex-col gap-space-10 pb-space-6">
      <HeroSection />

      {user && (usage?.run_count ?? 0) > 0 && <UsageSection data={usage!} />}

      {user && recentRuns.length > 0 && <RecentRunsSection runs={recentRuns} />}

      <CentresSection />

      <StepsSection />

      <FAQSection />

      {!user && <CTABanner />}
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
  return (
    <span className={cn('text-caption w-12 shrink-0 text-right', meta.text)}>{meta.label}</span>
  )
}
