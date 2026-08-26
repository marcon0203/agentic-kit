import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowLeft, Loader2, Send } from 'lucide-react'

import { ErrorPanel } from '@/components/common/EmptyState'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AgentWizardLayout } from '@/components/agents/AgentWizardLayout'
import { AgentWizardStepper } from '@/components/agents/AgentWizardStepper'
import { AgentWizardActions } from '@/components/agents/AgentWizardActions'
import { BasicInfoStep } from '@/components/agents/steps/BasicInfoStep'
import { PersonaKnowledgeStep } from '@/components/agents/steps/PersonaKnowledgeStep'
import { SkillsToolsStep } from '@/components/agents/steps/SkillsToolsStep'
import { ConstraintsHandoffStep } from '@/components/agents/steps/ConstraintsHandoffStep'
import { cn } from '@/lib/utils'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { useRunEvents } from '@/lib/runs/useRunEvents'
import { buildTimeline, type TimelineEntry } from '@/lib/runs/timeline'
import { PluginRenderCard } from '@/components/run/PluginRenderCard'
import { validateAgentDefinition } from '@/lib/validation/validateAgent'
import { EMPTY_FORM, formStateToDefinition, definitionToFormState, type FormState } from '@/lib/agents/definition'
import type { components } from '@/lib/api/schema'

type RunSummary = components['schemas']['RunSummary']
type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']
type AgentDefinition = components['schemas']['AgentDefinition']

const WIZARD_STEPS = [
  { id: 'basic', label: '基础信息', badge: '必填' },
  { id: 'persona', label: '人设与知识', badge: '核心' },
  { id: 'skills', label: '技能与工具', badge: '可选' },
  { id: 'constraints', label: '参数与协作', badge: '高级' },
] as const

/**
 * 步骤字段映射：点击“下一步”时只检查当前步骤相关的字段，
 * 避免第二步还没填就提示第四步的错误。
 */
const STEP_FIELDS: Record<number, string[]> = {
  0: ['agent', 'role', 'model.provider', 'model.name'],
  1: ['persona'],
  2: [],
  3: [],
}

function stepError(problems: { field: string; reason: string }[], step: number): string | null {
  const fields = STEP_FIELDS[step]
  if (!fields || fields.length === 0) return null
  const hit = problems.find((p) => fields.some((f) => p.field === f || p.field.startsWith(`${f}.`) || p.field.startsWith(`${f}[`)))
  return hit ? `${hit.field}：${hit.reason}` : null
}

function stepForField(field: string): number {
  for (let i = 0; i < WIZARD_STEPS.length; i++) {
    if (STEP_FIELDS[i].some((f) => field === f || field.startsWith(`${f}.`) || field.startsWith(`${f}[`))) {
      return i
    }
  }
  return 0
}

/**
 * 智能体配置向导：左侧垂直步骤条 + 右侧卡片式表单 + 右侧试运行面板。
 * 新建与编辑共用同一份表单态和保存逻辑。
 */
export function AgentStudioPage() {
  const navigate = useNavigate()
  const { ref } = useParams<{ ref?: string }>()
  const queryClient = useQueryClient()
  const isEdit = Boolean(ref)
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [currentStep, setCurrentStep] = useState(0)
  const [furthestStep, setFurthestStep] = useState(0)
  const [savedAt, setSavedAt] = useState<Date | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const versionsQuery = useQuery({
    queryKey: ['agent-versions', ref],
    queryFn: async () => unwrap<components['schemas']['Agent'][]>(await apiClient.GET('/agents/{ref}/versions', {
      params: { path: { ref: ref! } },
    })),
    enabled: isEdit,
  })

  const modelCatalogQuery = useQuery({
    queryKey: ['model-catalog'],
    queryFn: async () => unwrap<ModelCatalogEntry[]>(await apiClient.GET('/model-catalog', {})),
  })

  useEffect(() => {
    if (!isEdit || !versionsQuery.data) return
    const latest = versionsQuery.data[0]
    if (!latest) return
    setForm(definitionToFormState(latest.definition as AgentDefinition, false))
    setCurrentStep(0)
    setFurthestStep(0)
    setSavedAt(null)
  }, [isEdit, versionsQuery.data])

  useEffect(() => {
    if (isEdit) return
    setForm(EMPTY_FORM)
    setCurrentStep(0)
    setFurthestStep(0)
    setSavedAt(null)
  }, [isEdit])

  useEffect(() => {
    if (savedAt) return
    function guard(e: BeforeUnloadEvent) {
      e.preventDefault()
    }
    window.addEventListener('beforeunload', guard)
    return () => window.removeEventListener('beforeunload', guard)
  }, [savedAt])

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
    setSavedAt(null)
  }

  const definition = useMemo(() => formStateToDefinition(form), [form])
  const problems = useMemo(() => validateAgentDefinition(definition), [definition])

  const modelsForProvider = useMemo(
    () => (modelCatalogQuery.data ?? []).filter((e) => e.provider === form.provider),
    [modelCatalogQuery.data, form.provider],
  )

  function goNext() {
    const err = stepError(problems, currentStep)
    if (err) {
      setSaveError(err)
      return
    }
    setSaveError(null)
    const next = currentStep + 1
    setCurrentStep(next)
    setFurthestStep((f) => Math.max(f, next))
  }

  function goPrev() {
    if (currentStep > 0) {
      setCurrentStep((s) => s - 1)
      setSaveError(null)
    }
  }

  function goToStep(step: number) {
    // 只能回到已到达过的步骤，或当前步骤的下一步
    if (step <= Math.max(furthestStep, currentStep)) {
      setCurrentStep(step)
      setSaveError(null)
    }
  }

  async function save() {
    if (problems.length > 0) {
      const first = problems[0]
      setSaveError(`${first.field}：${first.reason}`)
      setCurrentStep(stepForField(first.field))
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      if (isEdit) {
        unwrap(
          await apiClient.PATCH('/agents/{ref}', {
            params: { path: { ref: ref! } },
            body: { definition },
          }),
        )
        queryClient.invalidateQueries({ queryKey: ['agents'] })
        queryClient.invalidateQueries({ queryKey: ['agent-versions', ref] })
        toast.success('已保存')
        navigate('/apps/agents')
      } else {
        unwrap(
          await apiClient.POST('/agents', {
            body: { definition },
            params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
          }),
        )
        setSavedAt(new Date())
        toast.success('已保存')
      }
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

  if (isEdit && versionsQuery.isLoading) {
    return (
      <div className="fixed inset-0 flex items-center justify-center bg-surface-page">
        <Loader2 className="size-8 animate-spin text-ink-500" />
      </div>
    )
  }

  if (isEdit && versionsQuery.isError) {
    return (
      <div className="fixed inset-0 flex flex-col items-center justify-center gap-space-4 bg-surface-page px-space-4">
        <ErrorPanel message="智能体数据加载失败" onRetry={() => versionsQuery.refetch()} />
        <Button variant="outline" size="sm" onClick={() => navigate('/apps/agents')}>
          返回列表
        </Button>
      </div>
    )
  }

  if (isEdit && versionsQuery.isSuccess && versionsQuery.data.length === 0) {
    return (
      <div className="fixed inset-0 flex flex-col items-center justify-center gap-space-4 bg-surface-page px-space-4">
        <ErrorPanel message="未找到该智能体" />
        <Button variant="outline" size="sm" onClick={() => navigate('/apps/agents')}>
          返回列表
        </Button>
      </div>
    )
  }

  const stepNode = (
    <AgentWizardStepper
      steps={WIZARD_STEPS.map((s) => ({ id: s.id, label: s.label }))}
      current={currentStep}
      completed={furthestStep}
      onChange={goToStep}
    />
  )

  const cardNode = (
    <Card className="border-border bg-surface shadow-sm">
      <CardHeader className="flex flex-row items-center justify-between border-b border-border pb-space-5">
        <CardTitle className="text-display-sm">{WIZARD_STEPS[currentStep].label}</CardTitle>
        <Badge variant="secondary">{WIZARD_STEPS[currentStep].badge}</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-space-6 pt-space-6">
        {currentStep === 0 && (
          <BasicInfoStep
            form={form}
            set={set}
            modelsForProvider={modelsForProvider}
            catalog={modelCatalogQuery.data ?? []}
            isEdit={isEdit}
          />
        )}
        {currentStep === 1 && <PersonaKnowledgeStep form={form} set={set} />}
        {currentStep === 2 && <SkillsToolsStep form={form} set={set} />}
        {currentStep === 3 && <ConstraintsHandoffStep form={form} set={set} />}

        {saveError && (
          <p role="alert" className="text-caption text-rust">{saveError}</p>
        )}

        <AgentWizardActions
          current={currentStep}
          total={WIZARD_STEPS.length}
          saving={saving}
          onPrev={goPrev}
          onNext={goNext}
          onSave={save}
        />
      </CardContent>
    </Card>
  )

  return (
    <div className="fixed inset-0 flex flex-col bg-surface-page">
      <header className="flex h-14 shrink-0 items-center gap-space-3 border-b border-border bg-surface px-space-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/agents')} aria-label="返回">
          <ArrowLeft className="size-4" aria-hidden />
        </Button>
        <span className="text-label-md truncate text-ink-900">
          {isEdit ? '编辑智能体' : form.role || form.agent || '新建智能体'}
        </span>
        <span className="text-caption shrink-0 rounded-sm bg-surface-muted px-space-2 py-0.5 text-ink-500">
          {isEdit ? '编辑模式' : savedAt ? '已保存' : '未保存'}
        </span>
        {savedAt && (
          <span className="text-caption text-ink-500">
            保存于 {savedAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}
          </span>
        )}
      </header>

      <AgentWizardLayout
        stepper={stepNode}
        card={cardNode}
        testPanel={<TestPanel form={form} problems={problems.map((p) => `${p.field}：${p.reason}`)} />}
      />
    </div>
  )
}

type RenderEntry = Extract<TimelineEntry, { kind: 'render' }>

interface Exchange {
  question: string
  answer: string
  failed: boolean
  // 一次试运行里触发的插件渲染器（spec-20 §4.2 的 node.render，比如图表
  // 渲染）——这个面板原来只读 timeline.bubbles 拼文本，node.render 事件
  // 被整个丢在地上：装了图表渲染器、也在 capabilities.tools[] 里引用了，
  // 模型也确实输出了 ```chart 代码块，运行完了却什么都看不到，问题不在
  // 触发条件，是这个面板压根没画这类事件。RunPage.tsx（正式运行详情页）
  // 一直是有画的，试运行这边漏了。
  renders: RenderEntry[]
}

/**
 * 右侧试运行面板。发出去的是当前这一刻的配置（POST /runs/agent-test 收的
 * 是完整定义而不是 ref），所以不用先保存就能测。
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
  const liveRenders = timeline.entries.filter((e): e is RenderEntry => e.kind === 'render')

  useEffect(() => {
    if (!runId || pending === null) return
    if (timeline.runStatus !== 'finished' && timeline.runStatus !== 'failed') return
    const failed = timeline.runStatus === 'failed'
    setHistory((h) => [
      ...h,
      {
        question: pending,
        answer: liveText || timeline.runError || (failed ? '运行失败' : '（没有输出）'),
        failed,
        renders: liveRenders,
      },
    ])
    setPending(null)
    setRunId(undefined)
  }, [timeline.runStatus, timeline.runError, runId, pending, liveText, liveRenders])

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
            {ex.renders.map((r) => (
              <PluginRenderCard key={r.key} entry={r} />
            ))}
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
            {liveRenders.map((r) => (
              <PluginRenderCard key={r.key} entry={r} />
            ))}
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
