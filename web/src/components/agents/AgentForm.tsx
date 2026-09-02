import { useEffect, useRef, useState } from 'react'
import { Check } from 'lucide-react'

import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { PluginToolMultiSelect } from '@/components/agents/PluginToolMultiSelect'
import { cn } from '@/lib/utils'
import { validateAgentDefinition, type FieldError } from '@/lib/validation/validateAgent'
import {
  BUILTIN_TOOLS,
  EMPTY_FORM,
  formStateToDefinition,
  type FormState,
} from '@/lib/agents/definition'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ProviderName = components['schemas']['ProviderName']

const STEPS = [
  { key: 'basic', title: '基本信息' },
  { key: 'model', title: '模型配置' },
  { key: 'persona', title: '角色设定' },
  { key: 'capabilities', title: '能力白名单' },
  { key: 'constraints', title: '执行约束' },
  { key: 'handoff', title: '协作契约' },
] as const
type StepKey = (typeof STEPS)[number]['key']

// Which validated field keys belong to each step — "下一步" only blocks on
// errors in the step the user is actually leaving, not on every field in
// the form (those still get caught at final submit).
const STEP_FIELDS: Record<StepKey, string[]> = {
  basic: ['agent', 'role', 'version'],
  model: ['model.name'],
  persona: ['persona'],
  capabilities: [],
  constraints: [],
  handoff: [],
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-space-4">
      <h2 className="text-display-md text-ink-900">{title}</h2>
      {children}
    </section>
  )
}

/**
 * The step rail. A wizard rather than one long scrolling form: each step
 * is small enough to actually read, and "下一步" catches a mistake in
 * 基本信息 before the user has typed three more sections past it.
 */
function StepRail({
  current,
  furthestValid,
  onJump,
}: {
  current: number
  furthestValid: number
  onJump: (index: number) => void
}) {
  return (
    <ol className="flex items-center gap-space-2">
      {STEPS.map((step, i) => {
        const state = i < current ? 'done' : i === current ? 'active' : 'upcoming'
        const reachable = i <= furthestValid
        return (
          <li key={step.key} className="flex items-center gap-space-2">
            <button
              type="button"
              disabled={!reachable}
              onClick={() => reachable && onJump(i)}
              className={cn(
                'flex items-center gap-space-2 rounded-full py-1 pl-1 pr-space-3 text-body-sm transition-colors',
                state === 'active' && 'bg-blueprint-tint font-medium text-blueprint',
                state === 'done' && 'text-ink-700 hover:bg-surface-muted',
                state === 'upcoming' && 'text-ink-500',
                !reachable && 'cursor-not-allowed opacity-60',
              )}
            >
              <span
                aria-hidden
                className={cn(
                  'flex size-6 shrink-0 items-center justify-center rounded-full text-caption',
                  state === 'active' && 'bg-blueprint text-white',
                  state === 'done' && 'bg-moss text-white',
                  state === 'upcoming' && 'bg-surface-muted text-ink-500',
                )}
              >
                {state === 'done' ? <Check className="size-3.5" /> : i + 1}
              </span>
              {step.title}
            </button>
            {i < STEPS.length - 1 && <span aria-hidden className="h-px w-space-5 bg-border" />}
          </li>
        )
      })}
    </ol>
  )
}

function Field({
  label,
  htmlFor,
  error,
  valid,
  helper,
  children,
}: {
  label: string
  htmlFor: string
  error?: string
  valid?: boolean
  helper?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-space-2">
      <label htmlFor={htmlFor} className="text-label-md text-ink-700">
        {label}
      </label>
      {children}
      {helper && !error && <p className="text-caption text-ink-500">{helper}</p>}
      {error && (
        <p id={`${htmlFor}-error`} className="text-caption text-rust">
          {error}
        </p>
      )}
      {!error && valid !== undefined && valid && <span className="sr-only">校验通过</span>}
    </div>
  )
}

export function AgentForm({
  initial,
  onSaved,
}: {
  initial?: FormState
  onSaved: () => void
}) {
  const [form, setForm] = useState<FormState>(initial ?? EMPTY_FORM)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [step, setStep] = useState(0)
  const [furthestValid, setFurthestValid] = useState(0)
  const formRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (initial) {
      setForm(initial)
      setStep(0)
      setFurthestValid(0)
      // "从现有 Agent 复制" — focus the ref field so the user notices the
      // -copy suffix and is prompted to change it.
      requestAnimationFrame(() => document.getElementById('agent-ref')?.focus())
    }
  }, [initial])

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function runValidation(f: FormState): FieldError[] {
    return validateAgentDefinition(formStateToDefinition(f))
  }

  function validateField(key: string) {
    setTouched((t) => ({ ...t, [key]: true }))
    const fieldErrors = runValidation(form)
    setErrors((prev) => {
      const next = { ...prev }
      const stillWrong = fieldErrors.find((e) => e.field === key)
      if (stillWrong) next[key] = stillWrong.reason
      else delete next[key]
      return next
    })
  }

  function stepIndexForField(key: string): number {
    const idx = STEPS.findIndex((s) => STEP_FIELDS[s.key].includes(key))
    return idx === -1 ? 0 : idx
  }

  function focusField(key: string) {
    requestAnimationFrame(() => {
      const el = document.getElementById(`agent-${key.split('.')[0].split('[')[0]}`) ?? formRef.current
      el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    })
  }

  // "下一步" only blocks on errors that belong to the step being left —
  // a mistake three steps ahead shouldn't stop you from moving past step 1.
  function goNext() {
    const stepKeys = STEP_FIELDS[STEPS[step].key]
    const fieldErrors = runValidation(form).filter((e) => stepKeys.includes(e.field))
    if (fieldErrors.length > 0) {
      setErrors((prev) => {
        const next = { ...prev }
        for (const e of fieldErrors) next[e.field] = e.reason
        return next
      })
      setTouched((t) => ({ ...t, ...Object.fromEntries(stepKeys.map((k) => [k, true])) }))
      return
    }
    const target = Math.min(step + 1, STEPS.length - 1)
    setStep(target)
    setFurthestValid((f) => Math.max(f, target))
  }

  function goPrev() {
    setStep((s) => Math.max(0, s - 1))
  }

  async function submit() {
    // Submit-time revalidation — never trust the earlier per-field passes.
    const fieldErrors = runValidation(form)
    if (fieldErrors.length > 0) {
      const map: Record<string, string> = {}
      for (const e of fieldErrors) map[e.field] = e.reason
      setErrors(map)
      const firstKey = fieldErrors[0].field
      const target = stepIndexForField(firstKey)
      setStep(target)
      setFurthestValid((f) => Math.max(f, target))
      focusField(firstKey)
      return
    }

    setPending(true)
    setSubmitError(null)
    try {
      unwrap(
        await apiClient.POST('/agents', {
          body: { definition: formStateToDefinition(form) },
          params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        }),
      )
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.details) {
        const map: Record<string, string> = {}
        for (const d of err.details) map[d.field] = d.reason
        setErrors(map)
      } else {
        setSubmitError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
      }
    } finally {
      setPending(false)
    }
  }

  function inputClass(key: string) {
    return cn(
      'h-12 rounded-sm',
      errors[key] && 'border-rust',
      !errors[key] && touched[key] && 'border-moss',
    )
  }

  return (
    <div ref={formRef} className="mx-auto flex w-full max-w-[720px] flex-col gap-space-8">
      <div className="overflow-x-auto pb-space-1">
        <StepRail current={step} furthestValid={furthestValid} onJump={setStep} />
      </div>

      {step === 0 && (
      <Section title="基本信息">
        <Field label="agent（唯一标识）" htmlFor="agent-agent" error={errors.agent} valid={touched.agent}>
          <Input
            id="agent-ref"
            value={form.agent}
            onChange={(e) => set('agent', e.target.value)}
            onBlur={() => validateField('agent')}
            className={inputClass('agent')}
            placeholder="architect"
          />
        </Field>
        <Field label="role（人类可读角色名）" htmlFor="agent-role" error={errors.role} valid={touched.role}>
          <Input
            id="agent-role"
            value={form.role}
            onChange={(e) => set('role', e.target.value)}
            onBlur={() => validateField('role')}
            className={inputClass('role')}
            placeholder="系统架构师"
          />
        </Field>
        <Field label="version" htmlFor="agent-version" error={errors.version} valid={touched.version}>
          <Input
            id="agent-version"
            value={form.version}
            onChange={(e) => set('version', e.target.value)}
            onBlur={() => validateField('version')}
            className={inputClass('version')}
            placeholder="1.0"
          />
        </Field>
      </Section>
      )}

      {step === 1 && (
      <Section title="模型配置">
        <Field label="provider" htmlFor="agent-model">
          <Select value={form.provider} onValueChange={(v) => set('provider', v as ProviderName)}>
            <SelectTrigger id="agent-model" className="h-12 w-full rounded-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="deepseek">deepseek</SelectItem>
              <SelectItem value="volcengine">volcengine</SelectItem>
              <SelectItem value="qwen">qwen</SelectItem>
              <SelectItem value="custom">custom</SelectItem>
              <SelectItem value="google">google</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="model name" htmlFor="agent-model-name" error={errors['model.name']}>
          <Input
            id="agent-model-name"
            value={form.modelName}
            onChange={(e) => set('modelName', e.target.value)}
            onBlur={() => validateField('model.name')}
            className={inputClass('model.name')}
            placeholder="deepseek-chat"
          />
        </Field>
        <Field label="fallback（逗号分隔，格式 provider/name）" htmlFor="agent-fallback" helper="主模型不可用时按顺序降级">
          <Input
            id="agent-fallback"
            value={form.fallback}
            onChange={(e) => set('fallback', e.target.value)}
            className="h-12 rounded-sm"
            placeholder="volcengine/doubao-seed-1-6, qwen/qwen-plus"
          />
        </Field>
        <Field label="temperature（可选，0-2）" htmlFor="agent-temperature">
          <Input
            id="agent-temperature"
            value={form.temperature}
            onChange={(e) => set('temperature', e.target.value)}
            className="h-12 rounded-sm"
            placeholder="0.4"
          />
        </Field>
      </Section>
      )}

      {step === 2 && (
      <Section title="角色设定">
        <Field
          label="persona"
          htmlFor="agent-persona"
          error={errors.persona}
          valid={touched.persona}
          helper="支持模板变量，如 {{shared_state.requirements}}"
        >
          <Textarea
            id="agent-persona"
            value={form.persona}
            onChange={(e) => set('persona', e.target.value)}
            onBlur={() => validateField('persona')}
            rows={5}
            className={cn(errors.persona && 'border-rust', !errors.persona && touched.persona && 'border-moss')}
          />
        </Field>
      </Section>
      )}

      {step === 3 && (
      <Section title="能力白名单">
        <Field label="tools" htmlFor="agent-tools">
          <div id="agent-tools" className="flex flex-col gap-space-3">
            <ResourceMultiSelect types={['tool', 'mcp', 'knowledge_base']} selected={form.tools} onChange={(v) => set('tools', v)} />
            <div>
              <p className="text-caption mb-space-2 text-ink-500">已安装的插件</p>
              <PluginToolMultiSelect selected={form.tools} onChange={(v) => set('tools', v)} />
            </div>
          </div>
        </Field>
        <Field label="skills" htmlFor="agent-skills">
          <ResourceMultiSelect types={['skill']} selected={form.skills} onChange={(v) => set('skills', v)} />
        </Field>
        <Field label="内置工具" htmlFor="agent-builtin-tools" helper="ADK 自带工具，不经过资源中心登记">
          <div id="agent-builtin-tools" className="flex flex-col gap-space-2">
            {BUILTIN_TOOLS.map((t) => (
              <label key={t.value} className="flex items-center gap-space-2 text-body-sm text-ink-900">
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
                />
                <span>
                  {t.label}
                  <span className="text-caption ml-space-2 text-ink-500">{t.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </Field>
        <Field label="hooks.before_tool_call" htmlFor="agent-hook-before-tool">
          <Input id="agent-hook-before-tool" value={form.hookBeforeToolCall} onChange={(e) => set('hookBeforeToolCall', e.target.value)} className="h-12 rounded-sm" placeholder="cost_guard, permission_check" />
        </Field>
        <Field label="hooks.after_tool_call" htmlFor="agent-hook-after-tool">
          <Input id="agent-hook-after-tool" value={form.hookAfterToolCall} onChange={(e) => set('hookAfterToolCall', e.target.value)} className="h-12 rounded-sm" />
        </Field>
        <Field label="hooks.before_response" htmlFor="agent-hook-before-response">
          <Input id="agent-hook-before-response" value={form.hookBeforeResponse} onChange={(e) => set('hookBeforeResponse', e.target.value)} className="h-12 rounded-sm" />
        </Field>
        <Field label="hooks.after_response" htmlFor="agent-hook-after-response">
          <Input id="agent-hook-after-response" value={form.hookAfterResponse} onChange={(e) => set('hookAfterResponse', e.target.value)} className="h-12 rounded-sm" placeholder="schema_validate" />
        </Field>
        <Field label="hooks.on_error" htmlFor="agent-hook-on-error">
          <Input id="agent-hook-on-error" value={form.hookOnError} onChange={(e) => set('hookOnError', e.target.value)} className="h-12 rounded-sm" />
        </Field>
      </Section>
      )}

      {step === 4 && (
      <Section title="执行约束">
        <Field label="max_tokens_per_turn" htmlFor="agent-max-tokens">
          <Input id="agent-max-tokens" value={form.maxTokensPerTurn} onChange={(e) => set('maxTokensPerTurn', e.target.value)} className="h-12 rounded-sm" placeholder="8000" />
        </Field>
        <Field label="max_tool_calls" htmlFor="agent-max-tool-calls">
          <Input id="agent-max-tool-calls" value={form.maxToolCalls} onChange={(e) => set('maxToolCalls', e.target.value)} className="h-12 rounded-sm" placeholder="15" />
        </Field>
        <Field label="max_turns" htmlFor="agent-max-turns">
          <Input id="agent-max-turns" value={form.maxTurns} onChange={(e) => set('maxTurns', e.target.value)} className="h-12 rounded-sm" placeholder="8" />
        </Field>
        <Field label="timeout_seconds" htmlFor="agent-timeout">
          <Input id="agent-timeout" value={form.timeoutSeconds} onChange={(e) => set('timeoutSeconds', e.target.value)} className="h-12 rounded-sm" placeholder="300" />
        </Field>
        <Field label="forbidden_actions（逗号分隔）" htmlFor="agent-forbidden">
          <Input id="agent-forbidden" value={form.forbiddenActions} onChange={(e) => set('forbiddenActions', e.target.value)} className="h-12 rounded-sm" placeholder="deploy_to_prod, delete_repo" />
        </Field>
        <Field label="output_schema" htmlFor="agent-output-schema">
          <Input id="agent-output-schema" value={form.outputSchema} onChange={(e) => set('outputSchema', e.target.value)} className="h-12 rounded-sm" placeholder="architecture_doc.schema.json" />
        </Field>
      </Section>
      )}

      {step === 5 && (
      <Section title="协作契约">
        <Field label="accepts_input_from（逗号分隔）" htmlFor="agent-accepts">
          <Input id="agent-accepts" value={form.acceptsInputFrom} onChange={(e) => set('acceptsInputFrom', e.target.value)} className="h-12 rounded-sm" placeholder="product_manager" />
        </Field>
        <Field label="produces_output_to（逗号分隔）" htmlFor="agent-produces">
          <Input id="agent-produces" value={form.producesOutputTo} onChange={(e) => set('producesOutputTo', e.target.value)} className="h-12 rounded-sm" placeholder="ui_designer, fullstack_engineer" />
        </Field>
        <div className="flex items-center gap-space-2">
          <Checkbox id="agent-requires-review" checked={form.requiresReview} onCheckedChange={(v) => set('requiresReview', v === true)} />
          <label htmlFor="agent-requires-review" className="text-label-md text-ink-700">
            requires_review
          </label>
        </div>
      </Section>
      )}

      {submitError && (
        <p role="alert" className="text-body-sm text-rust">
          {submitError}
        </p>
      )}

      <div className="flex items-center gap-space-3">
        {step > 0 && (
          <Button variant="outline" size="lg" onClick={goPrev} disabled={pending}>
            上一步
          </Button>
        )}
        {step < STEPS.length - 1 ? (
          <Button size="lg" onClick={goNext}>
            下一步
          </Button>
        ) : (
          <Button size="lg" disabled={pending} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        )}
      </div>
    </div>
  )
}

export { EMPTY_FORM }
