import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Boxes, Cpu, ShieldCheck, Store, Wrench } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type UsageSummary = components['schemas']['UsageSummary']
type RunSummary = components['schemas']['RunSummary']

const ENTRIES = [
  {
    to: '/apps',
    icon: Boxes,
    title: '应用中心',
    desc: '把多个 Agent 编排成一个 Bundle，可视化拖拽连线，一键发起运行。',
  },
  {
    to: '/resources',
    icon: Wrench,
    title: '资源中心',
    desc: '注册 Tool、Skill、MCP Server 与知识库，供 Agent 定义时引用。',
  },
  {
    to: '/models',
    icon: Cpu,
    title: '模型中心',
    desc: '接入 Anthropic / OpenAI / Google 等 Provider，凭证加密存储。',
  },
  {
    to: '/marketplace',
    icon: Store,
    title: '应用广场',
    desc: '订阅他人发布的 Agent 与 Bundle，黑盒分发，作者定义不泄露。',
  },
]

const WORKFLOW = [
  { title: '接入资源', desc: '注册 Tool / Skill / MCP / 知识库，并接入模型 Provider。' },
  { title: '定义 Agent', desc: '用 DSL 描述角色、人设、能力与执行约束，按版本管理。' },
  { title: '编排 Bundle', desc: '拖拽连线组成协作图，支持条件分支、并行与 human gate。' },
  { title: '运行与审批', desc: 'Chat 式实时时间线，关键节点停下来等人审批后再继续。' },
]

/** Abstract orchestration graph — the hero's feature visual. Uses the brand
 * gradient on the strokes only (design-system.md: gradient is for primary
 * buttons, key selected states and 特色视觉 — never a large body fill). */
function OrchestrationVisual() {
  return (
    <svg viewBox="0 0 420 300" className="h-auto w-full" role="img" aria-label="Bundle 编排示意图">
      <defs>
        <linearGradient id="portal-edge" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#365fff" />
          <stop offset="46%" stopColor="#655eff" />
          <stop offset="100%" stopColor="#9858ee" />
        </linearGradient>
      </defs>

      {/* edges: entry -> two parallel branches -> join -> gate -> end */}
      <g fill="none" stroke="url(#portal-edge)" strokeWidth="2">
        <path d="M118 66 C 160 66, 160 34, 202 34" />
        <path d="M118 66 C 160 66, 160 122, 202 122" />
        <path d="M318 34 C 356 34, 356 78, 356 78" />
        <path d="M318 122 C 356 122, 356 82, 356 82" />
        <path d="M356 82 C 356 150, 200 150, 200 196" />
      </g>
      <path d="M200 232 L 200 262" fill="none" stroke="var(--color-border-strong)" strokeWidth="2" strokeDasharray="5 5" />

      {/* nodes */}
      <g>
        <rect x="20" y="48" width="98" height="36" rx="18" fill="var(--color-surface)" stroke="var(--color-border-strong)" />
        <text x="69" y="71" textAnchor="middle" className="fill-ink-700" fontSize="13" fontWeight="600">
          product_mgr
        </text>

        <rect x="202" y="16" width="116" height="36" rx="18" fill="var(--color-surface)" stroke="var(--color-border-strong)" />
        <text x="260" y="39" textAnchor="middle" className="fill-ink-700" fontSize="13" fontWeight="600">
          architect
        </text>

        <rect x="202" y="104" width="116" height="36" rx="18" fill="var(--color-surface)" stroke="var(--color-border-strong)" />
        <text x="260" y="127" textAnchor="middle" className="fill-ink-700" fontSize="13" fontWeight="600">
          ui_designer
        </text>

        {/* human gate node — warning per design-system 状态映射 */}
        <rect x="132" y="196" width="136" height="36" rx="18" fill="var(--color-surface)" stroke="var(--color-warning)" strokeWidth="1.5" />
        <circle cx="156" cy="214" r="5" fill="var(--color-warning)" />
        <text x="208" y="219" textAnchor="middle" className="fill-ink-700" fontSize="13" fontWeight="600">
          待审批
        </text>

        <rect x="164" y="262" width="72" height="30" rx="15" fill="var(--color-surface-muted)" stroke="var(--color-border)" />
        <text x="200" y="282" textAnchor="middle" className="fill-ink-500" fontSize="12" fontWeight="700">
          END
        </text>
      </g>

      {/* parallel marker on the fan-out */}
      <g>
        <rect x="140" y="62" width="40" height="20" rx="10" fill="var(--color-surface-muted)" />
        <text x="160" y="76" textAnchor="middle" className="fill-ink-700" fontSize="11" fontWeight="700">
          并行
        </text>
      </g>
    </svg>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-space-1">
      <span className="text-data text-ink-900">{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

export function HomePage() {
  const user = useAuthStore((s) => s.user)
  const openModal = useAuthStore((s) => s.openModal)

  const usageQuery = useQuery({
    queryKey: ['usage-me', 'month'],
    queryFn: async () =>
      unwrap<UsageSummary>(await apiClient.GET('/usage/me', { params: { query: { period: 'month' } } })),
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
    <div className="flex flex-col gap-space-11 pb-space-10">
      {/* ── Hero ─────────────────────────────────────────────── */}
      <section
        className="relative overflow-hidden rounded-feature border border-border bg-surface px-space-10 py-space-10"
        style={{
          // Soft off-white tint only — design-system.md forbids the brand
          // gradient as a large body background.
          backgroundImage:
            'radial-gradient(120% 140% at 88% 8%, rgb(101 94 255 / 0.07) 0%, rgb(101 94 255 / 0) 58%), radial-gradient(90% 120% at 4% 96%, rgb(99 216 255 / 0.08) 0%, rgb(99 216 255 / 0) 52%)',
        }}
      >
        <div className="grid grid-cols-1 items-center gap-space-10 lg:grid-cols-[1.05fr_.95fr]">
          <div>
            <p className="text-eyebrow text-primary">AI AGENT 平台</p>
            {/* No max-width here: the grid column already constrains it, and
                a `ch` cap breaks 44px serif CJK mid-word (审/批). */}
            <h1 className="text-headline-lg mt-space-3 text-ink-900">
              {user ? `欢迎回来，${user.display_name}` : '编排、运行、审批你的 Agent Bundle'}
            </h1>
            <p className="text-body-lg mt-space-5 max-w-[54ch] text-ink-700">
              把多个 Agent 编排成一次可复现的协作：可视化拖拽连线生成 DSL，Chat
              式时间线实时回放每一步，关键节点停下来等人审批。做好的编排可以发布到广场，按黑盒分发给订阅者。
            </p>

            <div className="mt-space-8 flex flex-wrap items-center gap-space-3">
              {user ? (
                <Button asChild size="lg" className="rounded-full bg-[image:var(--gradient-brand)] px-space-7 hover:brightness-[1.04]">
                  <Link to="/apps">
                    进入应用中心
                    <ArrowRight className="size-4" aria-hidden />
                  </Link>
                </Button>
              ) : (
                <Button
                  size="lg"
                  className="rounded-full bg-[image:var(--gradient-brand)] px-space-7 hover:brightness-[1.04]"
                  onClick={() => openModal('manual')}
                >
                  登录 / 注册
                  <ArrowRight className="size-4" aria-hidden />
                </Button>
              )}
              <Button asChild variant="secondary" size="lg" className="rounded-full px-space-6">
                <Link to="/marketplace">浏览应用广场</Link>
              </Button>
            </div>
          </div>

          <div className="hidden rounded-panel border border-border bg-surface-page/60 p-space-7 lg:block">
            <OrchestrationVisual />
          </div>
        </div>
      </section>

      {/* ── 本月用量（登录后）────────────────────────────────── */}
      {user && (
        <section>
          <div className="mb-space-5 flex items-baseline justify-between gap-space-4">
            <h2 className="text-headline-sm text-ink-900">本月用量</h2>
            <Link to="/ops" className="text-body-sm font-medium text-primary hover:underline">
              查看运营中心
            </Link>
          </div>
          {/* Dividers bind the three metrics into one group — at full
              container width a plain 3-col grid spreads them so far apart
              they stop reading as a set (design-system.md 三: 留白用来区分
              层次，不是把同组信息拉开). */}
          <div className="grid grid-cols-1 rounded-panel border border-border bg-surface px-space-7 py-space-6 sm:grid-cols-3 sm:divide-x sm:divide-border">
            <div className="sm:pr-space-8">
              <Metric label="Token 消耗" value={(usageQuery.data?.total_tokens ?? 0).toLocaleString()} />
            </div>
            <div className="mt-space-6 sm:mt-0 sm:px-space-8">
              <Metric label="成本" value={`$${(usageQuery.data?.total_cost_usd ?? 0).toFixed(2)}`} />
            </div>
            <div className="mt-space-6 sm:mt-0 sm:px-space-8">
              <Metric label="运行次数" value={(usageQuery.data?.run_count ?? 0).toString()} />
            </div>
          </div>
        </section>
      )}

      {/* ── 快速入口 ──────────────────────────────────────────── */}
      <section>
        <h2 className="text-headline-sm mb-space-5 text-ink-900">快速入口</h2>
        <div className="grid grid-cols-1 gap-space-5 md:grid-cols-2 xl:grid-cols-4">
          {ENTRIES.map(({ to, icon: Icon, title, desc }) => (
            <Link
              key={to}
              to={to}
              className="group flex flex-col rounded-panel border border-border bg-surface p-space-7 transition-all duration-150 hover:-translate-y-1 hover:border-border-strong hover:shadow-md"
            >
              <span
                className="mb-space-5 inline-flex size-11 items-center justify-center rounded-sm text-white"
                style={{ backgroundImage: 'var(--gradient-brand)' }}
              >
                <Icon className="size-5" aria-hidden />
              </span>
              <span className="text-title-card text-ink-900">{title}</span>
              <span className="text-body-sm mt-space-2 flex-1 text-ink-700">{desc}</span>
              <span className="text-caption mt-space-5 inline-flex items-center gap-1 text-primary">
                进入
                <ArrowRight className="size-3 transition-transform duration-150 group-hover:translate-x-0.5" aria-hidden />
              </span>
            </Link>
          ))}
        </div>
      </section>

      {/* ── 平台工作流 ────────────────────────────────────────── */}
      <section>
        <h2 className="text-headline-sm mb-space-5 text-ink-900">四步跑通一次编排</h2>
        <div className="grid grid-cols-1 gap-space-5 md:grid-cols-2 xl:grid-cols-4">
          {WORKFLOW.map((step, i) => (
            <div key={step.title} className="rounded-panel border border-border bg-surface p-space-7">
              <div className="flex items-center gap-space-3">
                <span className="text-caption inline-flex size-7 items-center justify-center rounded-full bg-surface-muted text-ink-700">
                  {i + 1}
                </span>
                <span className="text-label-md text-ink-900">{step.title}</span>
              </div>
              <p className="text-body-sm mt-space-3 text-ink-700">{step.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* ── 最近运行（登录后且有数据）────────────────────────── */}
      {user && recentRuns.length > 0 && (
        <section>
          <div className="mb-space-5 flex items-baseline justify-between gap-space-4">
            <h2 className="text-headline-sm text-ink-900">最近运行</h2>
            <Link to="/ops" className="text-body-sm font-medium text-primary hover:underline">
              查看全部
            </Link>
          </div>
          <ul className="overflow-hidden rounded-panel border border-border bg-surface">
            {recentRuns.map((run) => (
              <li key={run.run_id} className="border-b border-border last:border-0">
                <Link
                  to={`/runs/${run.run_id}`}
                  className="flex items-center gap-space-4 px-space-7 py-space-4 transition-colors duration-150 hover:bg-surface-muted"
                >
                  <span className="text-body-md flex-1 text-ink-900">{run.bundle_ref}</span>
                  <span className="font-mono text-caption text-ink-500">{run.run_id}</span>
                  <RunStatusChip status={run.status} />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* ── 未登录收尾 CTA ────────────────────────────────────── */}
      {!user && (
        <section className="flex flex-col items-center gap-space-5 rounded-feature border border-dashed border-border-strong px-space-8 py-space-10 text-center">
          <ShieldCheck className="size-7 text-primary" aria-hidden />
          <h2 className="text-headline-sm text-ink-900">资源与凭证始终留在你自己的空间</h2>
          <p className="text-body-md max-w-[52ch] text-ink-700">
            模型凭证 AES-256-GCM 加密落库、接口一律不回显；发布到广场的资源默认黑盒，订阅者拿到的是能力，不是你的提示词与编排图。
          </p>
          <Button size="lg" className="rounded-full bg-[image:var(--gradient-brand)] px-space-7 hover:brightness-[1.04]" onClick={() => openModal('manual')}>
            创建账号
          </Button>
        </section>
      )}
    </div>
  )
}

/** Status pill — icon/text carries the state, never color alone
 * (design-system.md 1.2). */
function RunStatusChip({ status }: { status: RunSummary['status'] }) {
  const meta = {
    running: { label: '运行中', className: 'bg-primary/10 text-primary' },
    finished: {
      label: '已完成',
      className: 'bg-[color-mix(in_srgb,var(--color-success)_12%,transparent)] text-[var(--color-success)]',
    },
    failed: {
      label: '失败',
      className: 'bg-[color-mix(in_srgb,var(--color-error)_12%,transparent)] text-[var(--color-error)]',
    },
  }[status]

  return <span className={`text-caption shrink-0 rounded-full px-space-3 py-0.5 ${meta.className}`}>{meta.label}</span>
}
