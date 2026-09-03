import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowLeft, Copy, Loader2 } from 'lucide-react'

import { ErrorPanel } from '@/components/common/EmptyState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { TabRail, TabRailItem } from '@/components/common/Page'
import { AgentWizardLayout } from '@/components/agents/AgentWizardLayout'
import { AgentWizardStepper } from '@/components/agents/AgentWizardStepper'
import { AgentWizardActions } from '@/components/agents/AgentWizardActions'
import { BasicInfoStep } from '@/components/agents/steps/BasicInfoStep'
import { PersonaKnowledgeStep } from '@/components/agents/steps/PersonaKnowledgeStep'
import { SkillsToolsStep } from '@/components/agents/steps/SkillsToolsStep'
import { ConstraintsHandoffStep } from '@/components/agents/steps/ConstraintsHandoffStep'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { AgentThread } from '@/components/chat/AgentThread'
import { useConversation, type StartRun } from '@/lib/runs/useConversation'
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
  const { id } = useParams<{ id?: string }>()
  const queryClient = useQueryClient()
  const isEdit = Boolean(id)
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [currentStep, setCurrentStep] = useState(0)
  const [furthestStep, setFurthestStep] = useState(0)
  const [view, setView] = useState<'edit' | 'dsl' | 'versions'>('edit')
  const [savedAt, setSavedAt] = useState<Date | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const dslJson = useMemo(() => JSON.stringify(formStateToDefinition(form), null, 2), [form])

  const versionsQuery = useQuery({
    queryKey: ['agent-versions', id],
    queryFn: async () => unwrap<components['schemas']['Agent'][]>(await apiClient.GET('/agents/{id}/versions', {
      params: { path: { id: id! } },
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
          await apiClient.PATCH('/agents/{id}', {
            params: { path: { id: id! } },
            body: { definition },
          }),
        )
        queryClient.invalidateQueries({ queryKey: ['agents'] })
        queryClient.invalidateQueries({ queryKey: ['agent-versions', id] })
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

  const mainNode = (
    <div className="flex flex-col gap-space-4">
      <TabRail>
        <TabRailItem active={view === 'edit'} onClick={() => setView('edit')}>
          编排
        </TabRailItem>
        <TabRailItem active={view === 'dsl'} onClick={() => setView('dsl')}>
          查看 DSL
        </TabRailItem>
        {isEdit && (
          <TabRailItem active={view === 'versions'} onClick={() => setView('versions')}>
            版本管理
            {versionsQuery.data && versionsQuery.data.length > 0 ? (
              <span className="text-caption tabular text-ink-500">{versionsQuery.data.length}</span>
            ) : null}
          </TabRailItem>
        )}
      </TabRail>
      {view === 'edit' ? (
        cardNode
      ) : view === 'dsl' ? (
        <div className="flex flex-col gap-space-3">
          <div className="flex items-center justify-end">
            <Button variant="ghost" size="sm" onClick={() => navigator.clipboard?.writeText(dslJson)}>
              <Copy className="size-3.5" aria-hidden />
              复制
            </Button>
          </div>
          <pre className="text-body-sm max-h-[60vh] overflow-auto rounded-lg border border-border bg-surface p-space-4 whitespace-pre-wrap text-ink-900">
            {dslJson}
          </pre>
        </div>
      ) : (
        <div className="flex flex-col gap-space-3">
          {versionsQuery.isLoading && <p className="text-body-sm text-ink-500">加载版本列表…</p>}
          {versionsQuery.isError && <ErrorPanel message="版本列表没能加载出来" onRetry={() => versionsQuery.refetch()} />}
          {versionsQuery.isSuccess && (versionsQuery.data?.length ?? 0) === 0 && (
            <p className="text-body-sm text-ink-500">还没有保存过任何版本。</p>
          )}
          {versionsQuery.data && versionsQuery.data.length > 0 && (
            <ol className="overflow-hidden rounded-lg border border-border bg-surface">
              {versionsQuery.data.map((ver, i) => (
                <li
                  key={ver.version}
                  className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0"
                >
                  <span className="text-ref text-ink-900">v{ver.version}</span>
                  {i === 0 && (
                    <span className="text-caption rounded-full bg-blueprint-tint px-space-2 py-0.5 text-blueprint">
                      当前版本
                    </span>
                  )}
                  <span className="text-caption text-ink-500">
                    {new Date(ver.created_at).toLocaleString('zh-CN', {
                      month: '2-digit',
                      day: '2-digit',
                      hour: '2-digit',
                      minute: '2-digit',
                    })}
                  </span>
                  <span className="text-caption truncate text-ink-500">
                    {ver.definition?.role || '—'}
                  </span>
                  {i !== 0 && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="ml-auto"
                      onClick={() => {
                        setForm(definitionToFormState(ver.definition as AgentDefinition, false))
                        setCurrentStep(0)
                        setFurthestStep(0)
                        setView('edit')
                      }}
                    >
                      载入此版本
                    </Button>
                  )}
                </li>
              ))}
            </ol>
          )}
          <p className="text-caption text-ink-500">
            载入历史版本到编辑器后不会自动保存——改完记得点「保存」会创建一个新版本。
          </p>
        </div>
      )}
    </div>
  )

  return (
    <div className="fixed inset-0 flex flex-col bg-surface-page">
      <header className="flex h-14 shrink-0 items-center gap-space-3 border-b border-border bg-surface px-space-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/agents')} aria-label="返回">
          <ArrowLeft className="size-4" aria-hidden />
        </Button>
        <span className="text-label-md truncate text-ink-900">
          {isEdit ? form.role || form.agent || '编辑智能体' : form.role || form.agent || '新建智能体'}
        </span>
        {isEdit && versionsQuery.data?.[0]?.version && (
          <span className="text-caption shrink-0 rounded-sm bg-blueprint-tint px-space-2 py-0.5 text-blueprint">
            v{versionsQuery.data[0].version}
          </span>
        )}
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
        card={mainNode}
        testPanel={<TestPanel form={form} problems={problems.map((p) => `${p.field}：${p.reason}`)} />}
      />
    </div>
  )
}

/**
 * 右侧试运行面板。发出去的是当前这一刻的配置（POST /runs/agent-test 收的
 * 是完整定义而不是 ref），所以不用先保存就能测。
 *
 * 界面是 assistant-ui 的线程；对话状态和事件流仍走 useConversation，
 * 它把连续几条消息串成同一段会话（带上 session_id），模型因此接得住上文
 * ——在这之前每发一条都是全新运行，问"接着上一句说"只会得到一脸茫然。
 */
function TestPanel({ form, problems }: { form: FormState; problems: string[] }) {
  const blocked = problems.length > 0

  // form 每敲一个键就换引用，start 直接依赖它会让 useConversation 的
  // send 每帧都换新的。用 ref 读最新值：发送那一刻取的就是当下的配置。
  const formRef = useRef(form)
  formRef.current = form

  const start = useCallback<StartRun>(async (question, sessionID) => {
    const created = unwrap<RunSummary>(
      await apiClient.POST('/runs/agent-test', {
        body: {
          definition: formStateToDefinition(formRef.current),
          input: { message: question },
          ...(sessionID ? { session_id: sessionID } : {}),
        },
        params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
      }),
    )
    return created
  }, [])

  const chat = useConversation({ start, blocked })

  return (
    <aside className="flex w-[420px] shrink-0 flex-col border-l border-border bg-surface">
      <AgentThread
        messages={chat.messages}
        isRunning={chat.isRunning}
        onSend={chat.send}
        disabled={blocked}
        disabledHint={blocked ? `配置还不完整，补齐后即可试运行：${problems[0]}` : undefined}
        emptyTitle={form.role || form.agent || '新建智能体'}
        emptyHint="输入问题进行测试体验"
        header={
          <div className="flex h-12 shrink-0 items-center justify-between border-b border-border px-space-4">
            <span className="text-label-md text-ink-900">试运行</span>
            <span className="text-caption text-ink-500">连续发送会接着同一段对话</span>
          </div>
        }
        footerNote={
          chat.error ? (
            <p role="alert" className="text-caption text-rust">
              {chat.error}
            </p>
          ) : undefined
        }
      />
    </aside>
  )
}
