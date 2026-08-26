import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { ArrowLeft, Blocks, BookOpen, Brain, LayoutGrid, MessageSquare, Send, Share2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { PluginToolMultiSelect } from '@/components/agents/PluginToolMultiSelect'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { useFeatures } from '@/lib/features/useFeatures'
import { useRunEvents } from '@/lib/runs/useRunEvents'
import { buildTimeline } from '@/lib/runs/timeline'
import { validateAgentDefinition } from '@/lib/validation/validateAgent'
import {
  BUILTIN_TOOLS,
  EMPTY_FORM,
  formStateToDefinition,
  type FormState,
} from '@/lib/agents/definition'
import type { components } from '@/lib/api/schema'

type ProviderName = components['schemas']['ProviderName']
type RunSummary = components['schemas']['RunSummary']

const SECTIONS = [
  { id: 'planning', label: '规划', icon: LayoutGrid },
  { id: 'knowledge', label: '知识', icon: BookOpen },
  { id: 'skills', label: '技能', icon: Blocks },
  { id: 'memory', label: '记忆', icon: Brain },
  { id: 'reply', label: '回复', icon: MessageSquare },
  { id: 'handoff', label: '协作', icon: Share2 },
] as const

const PROVIDERS: ProviderName[] = ['anthropic', 'openai', 'google', 'deepseek', 'qwen', 'custom']

/**
 * 智能体工作台：左边配置、右边实时测试，中间的配置区按 规划/知识/技能/
 * 记忆/回复/协作 分段，左侧导航跟着滚动位置走。
 *
 * 和原来的分步向导（AgentForm）是两种不同的编辑姿势，不是同一个东西的两
 * 张皮：向导适合"第一次建，别漏字段"，工作台适合"改一句提示词，右边立刻
 * 看效果"——后者只有把配置和测试摆在同一屏里才成立。两边共用同一份表单态
 * 与 DSL 转换（lib/agents/definition），所以填出来的东西是一样的。
 */
export function AgentStudioPage() {
  const navigate = useNavigate()
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [active, setActive] = useState<string>('planning')
  const [savedAt, setSavedAt] = useState<Date | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const { knowledgeBaseEnabled } = useFeatures()

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
    // 任何一次改动都让"已保存"退回"未发布"——顶栏那个状态必须说实话。
    setSavedAt(null)
  }

  const definition = useMemo(() => formStateToDefinition(form), [form])
  const problems = useMemo(() => validateAgentDefinition(definition), [definition])

  // 左侧导航跟随滚动：观察每个 section，取当前最靠上的那个。
  useEffect(() => {
    const root = scrollRef.current
    if (!root) return
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible[0]) setActive(visible[0].target.id.replace('section-', ''))
      },
      { root, rootMargin: '-10% 0px -70% 0px', threshold: 0 },
    )
    for (const s of SECTIONS) {
      const el = document.getElementById(`section-${s.id}`)
      if (el) observer.observe(el)
    }
    return () => observer.disconnect()
  }, [knowledgeBaseEnabled])

  function jumpTo(id: string) {
    document.getElementById(`section-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  async function save() {
    if (problems.length > 0) {
      setSaveError(`${problems[0].field}：${problems[0].reason}`)
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      unwrap(
        await apiClient.POST('/agents', {
          body: { definition },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      setSavedAt(new Date())
      toast.success('已保存')
    } catch (err) {
      if (err instanceof ApiError && err.details?.length) {
        setSaveError(`${err.details[0].field}：${err.details[0].reason}`)
      } else {
        setSaveError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 flex flex-col bg-surface-page">
      <header className="flex h-14 shrink-0 items-center gap-space-3 border-b border-border bg-surface px-space-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/agents')} aria-label="返回">
          <ArrowLeft className="size-4" aria-hidden />
        </Button>
        <span className="text-label-md truncate text-ink-900">{form.role || form.agent || '新建智能体'}</span>
        <span className="text-caption shrink-0 rounded-sm bg-surface-muted px-space-2 py-0.5 text-ink-500">
          {savedAt ? '已保存' : '未保存'}
        </span>
        {savedAt && (
          <span className="text-caption text-ink-500">
            保存于 {savedAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}
          </span>
        )}

        <div className="flex-1" />

        {saveError && (
          <p role="alert" className="text-caption max-w-[420px] truncate text-rust" title={saveError}>
            {saveError}
          </p>
        )}
        <Button
          size="sm"
          disabled={saving}
          onClick={save}
          className="bg-gradient-cta text-white hover:opacity-90"
        >
          {saving ? '保存中…' : '保存'}
        </Button>
      </header>

      <div className="flex min-h-0 flex-1">
        <nav className="flex w-[76px] shrink-0 flex-col gap-space-1 border-r border-border bg-surface py-space-3">
          {SECTIONS.map((s) => {
            const Icon = s.icon
            return (
              <button
                key={s.id}
                type="button"
                aria-current={active === s.id ? 'true' : undefined}
                onClick={() => jumpTo(s.id)}
                className={cn(
                  'flex flex-col items-center gap-1 py-space-2 transition-colors',
                  active === s.id ? 'text-blueprint' : 'text-ink-500 hover:text-ink-900',
                )}
              >
                <Icon className="size-4" aria-hidden />
                <span className="text-caption">{s.label}</span>
              </button>
            )
          })}
        </nav>

        <div ref={scrollRef} className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto flex max-w-[760px] flex-col gap-space-8 px-space-6 py-space-6">
            <StudioSection id="planning" title="规划">
              <Field label="agent（唯一标识）" htmlFor="studio-agent">
                <Input
                  id="studio-agent"
                  value={form.agent}
                  onChange={(e) => set('agent', e.target.value)}
                  placeholder="architect"
                />
              </Field>
              <Field label="role（人类可读角色名）" htmlFor="studio-role">
                <Input
                  id="studio-role"
                  value={form.role}
                  onChange={(e) => set('role', e.target.value)}
                  placeholder="系统架构师"
                />
              </Field>
              <div className="grid grid-cols-2 gap-space-4">
                <Field label="模型 provider" htmlFor="studio-provider">
                  <Select value={form.provider} onValueChange={(v) => set('provider', v as ProviderName)}>
                    <SelectTrigger id="studio-provider" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PROVIDERS.map((p) => (
                        <SelectItem key={p} value={p}>
                          {p}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="模型名称" htmlFor="studio-model">
                  <Input
                    id="studio-model"
                    value={form.modelName}
                    onChange={(e) => set('modelName', e.target.value)}
                    placeholder="claude-sonnet-5"
                  />
                </Field>
              </div>
              <div className="grid grid-cols-2 gap-space-4">
                <Field
                  label="fallback（逗号分隔）"
                  htmlFor="studio-fallback"
                  helper="主模型不可用时按顺序降级，格式 provider/name"
                >
                  <Input
                    id="studio-fallback"
                    value={form.fallback}
                    onChange={(e) => set('fallback', e.target.value)}
                    placeholder="openai/gpt-5"
                  />
                </Field>
                <Field label="temperature（可选，0-2）" htmlFor="studio-temperature">
                  <Input
                    id="studio-temperature"
                    value={form.temperature}
                    onChange={(e) => set('temperature', e.target.value)}
                    placeholder="0.4"
                  />
                </Field>
              </div>
              <Field
                label="提示词（persona）"
                htmlFor="studio-persona"
                helper="设定这个智能体是谁、任务是什么、按什么步骤做、有哪些不能做。"
              >
                <div className="relative">
                  <Textarea
                    id="studio-persona"
                    value={form.persona}
                    onChange={(e) => set('persona', e.target.value)}
                    rows={12}
                    className="resize-y"
                  />
                  <span className="text-caption pointer-events-none absolute right-3 bottom-2 text-ink-500">
                    {form.persona.length}
                  </span>
                </div>
              </Field>
            </StudioSection>

            <StudioSection id="knowledge" title="知识">
              {knowledgeBaseEnabled ? (
                <Field
                  label="引用的知识库"
                  htmlFor="studio-kb"
                  helper="被引用的知识库会作为一个可调用的检索工具进入这个智能体的能力白名单。"
                >
                  <ResourceMultiSelect
                    types={['knowledge_base']}
                    selected={form.tools}
                    onChange={(refs) => set('tools', refs)}
                  />
                </Field>
              ) : (
                <p className="text-body-sm text-ink-500">
                  知识库功能未启用（KB_ENABLED），这台服务器上暂时不能给智能体挂知识库。
                </p>
              )}
            </StudioSection>

            <StudioSection id="skills" title="技能">
              <Field label="组件" htmlFor="studio-tools" helper="HTTP 接口、OpenAPI 导入的操作、沙箱环境、MCP Server。">
                <ResourceMultiSelect
                  types={['tool', 'mcp']}
                  selected={form.tools}
                  onChange={(refs) => set('tools', refs)}
                />
              </Field>
              <Field label="插件" htmlFor="studio-plugin-tools" helper="已安装插件暴露的工具，装了插件却没在这里看到，先去组件广场的「插件」Tab 确认已安装。">
                <PluginToolMultiSelect selected={form.tools} onChange={(refs) => set('tools', refs)} />
              </Field>
              <Field label="Skill" htmlFor="studio-skills" helper="一段打包好的固定做法，调用时把步骤交给模型照做。">
                <ResourceMultiSelect types={['skill']} selected={form.skills} onChange={(refs) => set('skills', refs)} />
              </Field>
              <Field label="内置工具" htmlFor="studio-builtin" helper="ADK 自带的实现，不需要在资源中心注册。">
                <div className="flex flex-col gap-space-2">
                  {BUILTIN_TOOLS.map((t) => (
                    <label key={t.value} className="flex items-start gap-space-3">
                      <Checkbox
                        checked={form.builtinTools.includes(t.value)}
                        onCheckedChange={(checked) =>
                          set(
                            'builtinTools',
                            checked === true
                              ? [...form.builtinTools, t.value]
                              : form.builtinTools.filter((v) => v !== t.value),
                          )
                        }
                        className="mt-0.5"
                      />
                      <span className="flex flex-col">
                        <span className="text-ref text-ink-900">{t.label}</span>
                        <span className="text-caption text-ink-500">{t.hint}</span>
                      </span>
                    </label>
                  ))}
                </div>
              </Field>
            </StudioSection>

            <StudioSection id="memory" title="记忆">
              <Field
                label="引用的记忆库"
                htmlFor="studio-memory"
                helper="挂上记忆库后，还要在上面的内置工具里勾选 load_memory 或 preload_memory，模型才会真的去检索它。"
              >
                <ResourceMultiSelect
                  types={['memory']}
                  selected={form.tools}
                  onChange={(refs) => set('tools', refs)}
                />
              </Field>
            </StudioSection>

            <StudioSection id="reply" title="回复">
              <div className="grid grid-cols-2 gap-space-4">
                <Field label="单轮最大 token" htmlFor="studio-max-tokens">
                  <Input
                    id="studio-max-tokens"
                    value={form.maxTokensPerTurn}
                    onChange={(e) => set('maxTokensPerTurn', e.target.value)}
                    placeholder="4000"
                  />
                </Field>
                <Field label="最大工具调用次数" htmlFor="studio-max-tool-calls">
                  <Input
                    id="studio-max-tool-calls"
                    value={form.maxToolCalls}
                    onChange={(e) => set('maxToolCalls', e.target.value)}
                    placeholder="10"
                  />
                </Field>
                <Field label="最大轮次" htmlFor="studio-max-turns">
                  <Input
                    id="studio-max-turns"
                    value={form.maxTurns}
                    onChange={(e) => set('maxTurns', e.target.value)}
                    placeholder="6"
                  />
                </Field>
                <Field label="超时（秒）" htmlFor="studio-timeout">
                  <Input
                    id="studio-timeout"
                    value={form.timeoutSeconds}
                    onChange={(e) => set('timeoutSeconds', e.target.value)}
                    placeholder="120"
                  />
                </Field>
              </div>
              <Field label="禁止的动作（逗号分隔）" htmlFor="studio-forbidden">
                <Input
                  id="studio-forbidden"
                  value={form.forbiddenActions}
                  onChange={(e) => set('forbiddenActions', e.target.value)}
                  placeholder="delete_production_data"
                />
              </Field>
              <Field label="输出 schema（可选）" htmlFor="studio-output-schema" helper="填了就要求模型按这个结构回复。">
                <Textarea
                  id="studio-output-schema"
                  value={form.outputSchema}
                  onChange={(e) => set('outputSchema', e.target.value)}
                  rows={4}
                  className="font-mono"
                />
              </Field>
            </StudioSection>

            <StudioSection id="handoff" title="协作">
              <Field label="接受哪些节点的输入（逗号分隔）" htmlFor="studio-accepts">
                <Input
                  id="studio-accepts"
                  value={form.acceptsInputFrom}
                  onChange={(e) => set('acceptsInputFrom', e.target.value)}
                  placeholder="researcher"
                />
              </Field>
              <Field label="输出交给哪些节点（逗号分隔）" htmlFor="studio-produces">
                <Input
                  id="studio-produces"
                  value={form.producesOutputTo}
                  onChange={(e) => set('producesOutputTo', e.target.value)}
                  placeholder="reviewer"
                />
              </Field>
              <label className="flex items-center gap-space-3">
                <Checkbox
                  checked={form.requiresReview}
                  onCheckedChange={(checked) => set('requiresReview', checked === true)}
                />
                <span className="text-body-sm text-ink-700">输出需要人工确认后才继续</span>
              </label>
            </StudioSection>
          </div>
        </div>

        <TestPanel form={form} problems={problems.map((p) => `${p.field}：${p.reason}`)} />
      </div>
    </div>
  )
}

function StudioSection({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section id={`section-${id}`} className="flex scroll-mt-space-4 flex-col gap-space-4">
      <h2 className="text-display-sm text-ink-900">{title}</h2>
      {children}
    </section>
  )
}

function Field({
  label,
  htmlFor,
  helper,
  children,
}: {
  label: string
  htmlFor: string
  helper?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-space-2">
      <label htmlFor={htmlFor} className="text-label-md text-ink-700">
        {label}
      </label>
      {children}
      {helper && <p className="text-caption text-ink-500">{helper}</p>}
    </div>
  )
}

interface Exchange {
  question: string
  answer: string
  failed: boolean
}

/**
 * 右侧试运行面板。发出去的是当前这一刻的配置（POST /runs/agent-test 收的
 * 是完整定义而不是 ref），所以不用先保存就能测。
 *
 * 每次发送都是一次独立运行，不是一段连续对话——平台的运行本来就是一次性
 * 的，把它画成聊天框是为了输入顺手，不是在暗示有上下文。
 */
function TestPanel({ form, problems }: { form: FormState; problems: string[] }) {
  const [history, setHistory] = useState<Exchange[]>([])
  const [draft, setDraft] = useState('')
  const [runId, setRunId] = useState<string | undefined>(undefined)
  const [pending, setPending] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const { events } = useRunEvents(runId)
  const timeline = useMemo(() => buildTimeline(events), [events])
  const liveText = Object.values(timeline.bubbles)
    .map((b) => b.text)
    .filter(Boolean)
    .join('\n')

  // 一次运行结束就把它归档进 history，让面板回到可以再发一条的状态。
  useEffect(() => {
    if (!runId || pending === null) return
    if (timeline.runStatus !== 'finished' && timeline.runStatus !== 'failed') return
    const failed = timeline.runStatus === 'failed'
    setHistory((h) => [
      ...h,
      { question: pending, answer: liveText || timeline.runError || (failed ? '运行失败' : '（没有输出）'), failed },
    ])
    setPending(null)
    setRunId(undefined)
  }, [timeline.runStatus, timeline.runError, runId, pending, liveText])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [history.length, liveText])

  const blocked = problems.length > 0
  const busy = starting || pending !== null

  async function send() {
    const question = draft.trim()
    if (!question || blocked || busy) return
    setDraft('')
    setError(null)
    setStarting(true)
    try {
      const created = unwrap<RunSummary>(
        await apiClient.POST('/runs/agent-test', {
          body: { definition: formStateToDefinition(form), input: { message: question } },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      setPending(question)
      setRunId(created.run_id)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '试运行没能启动，请稍后重试')
      setDraft(question)
    } finally {
      setStarting(false)
    }
  }

  return (
    <aside className="flex w-[420px] shrink-0 flex-col border-l border-border bg-surface">
      <div className="flex h-12 shrink-0 items-center justify-between border-b border-border px-space-4">
        <span className="text-label-md text-ink-900">试运行</span>
        <span className="text-caption text-ink-500">每次发送都是一次独立运行</span>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-space-4 overflow-y-auto px-space-4 py-space-4">
        {history.length === 0 && pending === null && (
          <div className="flex flex-1 flex-col items-center justify-center gap-space-2 text-center">
            <span className="text-label-md text-ink-900">{form.role || form.agent || '新建智能体'}</span>
            <span className="text-body-sm text-ink-500">输入问题进行测试体验</span>
          </div>
        )}

        {history.map((ex, i) => (
          <div key={i} className="flex flex-col gap-space-2">
            <p className="text-body-sm ml-auto max-w-[85%] rounded-lg bg-blueprint-tint px-space-3 py-space-2 text-ink-900">
              {ex.question}
            </p>
            <p
              className={cn(
                'text-body-sm max-w-[85%] rounded-lg px-space-3 py-space-2 whitespace-pre-wrap',
                ex.failed ? 'bg-rust-tint text-rust' : 'bg-surface-muted text-ink-900',
              )}
            >
              {ex.answer}
            </p>
          </div>
        ))}

        {pending !== null && (
          <div className="flex flex-col gap-space-2">
            <p className="text-body-sm ml-auto max-w-[85%] rounded-lg bg-blueprint-tint px-space-3 py-space-2 text-ink-900">
              {pending}
            </p>
            <p
              aria-busy
              className="text-body-sm max-w-[85%] rounded-lg bg-surface-muted px-space-3 py-space-2 whitespace-pre-wrap text-ink-900"
            >
              {liveText || '运行中…'}
            </p>
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      {blocked && (
        <p className="text-caption border-t border-border px-space-4 py-space-2 text-ink-500">
          配置还不完整，补齐后即可试运行：{problems[0]}
        </p>
      )}
      {error && (
        <p role="alert" className="text-caption border-t border-border px-space-4 py-space-2 text-rust">
          {error}
        </p>
      )}

      <div className="flex shrink-0 items-end gap-space-2 border-t border-border p-space-3">
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              send()
            }
          }}
          rows={2}
          placeholder={blocked ? '补齐配置后可试运行' : '输入问题，Enter 发送'}
          aria-label="试运行输入"
          disabled={blocked}
          className="resize-none"
        />
        <Button size="sm" disabled={blocked || busy || !draft.trim()} onClick={send} aria-label="发送">
          <Send className="size-4" aria-hidden />
        </Button>
      </div>
    </aside>
  )
}
