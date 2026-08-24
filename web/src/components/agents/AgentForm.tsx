import { useEffect, useRef, useState } from 'react'

import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { cn } from '@/lib/utils'
import { validateAgentDefinition, type FieldError } from '@/lib/validation/validateAgent'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type AgentDefinition = components['schemas']['AgentDefinition']
type ProviderName = components['schemas']['ProviderName']

interface FormState {
  agent: string
  role: string
  version: string
  provider: ProviderName
  modelName: string
  fallback: string
  temperature: string
  persona: string
  tools: string[]
  skills: string[]
  hookBeforeToolCall: string
  hookAfterToolCall: string
  hookBeforeResponse: string
  hookAfterResponse: string
  hookOnError: string
  maxTokensPerTurn: string
  maxToolCalls: string
  maxTurns: string
  timeoutSeconds: string
  forbiddenActions: string
  outputSchema: string
  acceptsInputFrom: string
  producesOutputTo: string
  requiresReview: boolean
}

const EMPTY_FORM: FormState = {
  agent: '',
  role: '',
  version: '1.0',
  provider: 'anthropic',
  modelName: '',
  fallback: '',
  temperature: '',
  persona: '',
  tools: [],
  skills: [],
  hookBeforeToolCall: '',
  hookAfterToolCall: '',
  hookBeforeResponse: '',
  hookAfterResponse: '',
  hookOnError: '',
  maxTokensPerTurn: '',
  maxToolCalls: '',
  maxTurns: '',
  timeoutSeconds: '',
  forbiddenActions: '',
  outputSchema: '',
  acceptsInputFrom: '',
  producesOutputTo: '',
  requiresReview: false,
}

function csv(s: string): string[] {
  return s
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean)
}

export function definitionToFormState(def: AgentDefinition, copySuffix = false): FormState {
  return {
    agent: copySuffix ? `${def.agent}-copy` : def.agent,
    role: def.role,
    version: def.version,
    provider: def.model.provider,
    modelName: def.model.name,
    fallback: (def.model.fallback ?? []).join(', '),
    temperature: def.model.temperature?.toString() ?? '',
    persona: def.persona,
    tools: def.capabilities?.tools ?? [],
    skills: def.capabilities?.skills ?? [],
    hookBeforeToolCall: (def.capabilities?.hooks?.before_tool_call ?? []).join(', '),
    hookAfterToolCall: (def.capabilities?.hooks?.after_tool_call ?? []).join(', '),
    hookBeforeResponse: (def.capabilities?.hooks?.before_response ?? []).join(', '),
    hookAfterResponse: (def.capabilities?.hooks?.after_response ?? []).join(', '),
    hookOnError: (def.capabilities?.hooks?.on_error ?? []).join(', '),
    maxTokensPerTurn: def.constraints?.max_tokens_per_turn?.toString() ?? '',
    maxToolCalls: def.constraints?.max_tool_calls?.toString() ?? '',
    maxTurns: def.constraints?.max_turns?.toString() ?? '',
    timeoutSeconds: def.constraints?.timeout_seconds?.toString() ?? '',
    forbiddenActions: (def.constraints?.forbidden_actions ?? []).join(', '),
    outputSchema: def.constraints?.output_schema ?? '',
    acceptsInputFrom: (def.handoff?.accepts_input_from ?? []).join(', '),
    producesOutputTo: (def.handoff?.produces_output_to ?? []).join(', '),
    requiresReview: def.handoff?.requires_review ?? false,
  }
}

function formStateToDefinition(f: FormState): AgentDefinition {
  return {
    agent: f.agent,
    role: f.role,
    version: f.version,
    model: {
      provider: f.provider,
      name: f.modelName,
      ...(csv(f.fallback).length ? { fallback: csv(f.fallback) } : {}),
      ...(f.temperature ? { temperature: Number(f.temperature) } : {}),
    },
    persona: f.persona,
    capabilities: {
      tools: f.tools,
      skills: f.skills,
      hooks: {
        before_tool_call: csv(f.hookBeforeToolCall),
        after_tool_call: csv(f.hookAfterToolCall),
        before_response: csv(f.hookBeforeResponse),
        after_response: csv(f.hookAfterResponse),
        on_error: csv(f.hookOnError),
      },
    },
    constraints: {
      ...(f.maxTokensPerTurn ? { max_tokens_per_turn: Number(f.maxTokensPerTurn) } : {}),
      ...(f.maxToolCalls ? { max_tool_calls: Number(f.maxToolCalls) } : {}),
      ...(f.maxTurns ? { max_turns: Number(f.maxTurns) } : {}),
      ...(f.timeoutSeconds ? { timeout_seconds: Number(f.timeoutSeconds) } : {}),
      ...(csv(f.forbiddenActions).length ? { forbidden_actions: csv(f.forbiddenActions) } : {}),
      ...(f.outputSchema ? { output_schema: f.outputSchema } : {}),
    },
    handoff: {
      accepts_input_from: csv(f.acceptsInputFrom),
      produces_output_to: csv(f.producesOutputTo),
      requires_review: f.requiresReview,
    },
  }
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-space-4 border-t border-border pt-space-7 first:border-t-0 first:pt-0">
      <h2 className="text-display-md text-ink-900">{title}</h2>
      {children}
    </section>
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
  const formRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (initial) {
      setForm(initial)
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

  async function submit() {
    // Submit-time revalidation — never trust the earlier per-field passes.
    const fieldErrors = runValidation(form)
    if (fieldErrors.length > 0) {
      const map: Record<string, string> = {}
      for (const e of fieldErrors) map[e.field] = e.reason
      setErrors(map)
      const firstKey = fieldErrors[0].field
      const el = document.getElementById(`agent-${firstKey.split('.')[0].split('[')[0]}`) ?? formRef.current
      el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
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

      <Section title="模型配置">
        <Field label="provider" htmlFor="agent-model">
          <Select value={form.provider} onValueChange={(v) => set('provider', v as ProviderName)}>
            <SelectTrigger id="agent-model" className="h-12 w-full rounded-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="anthropic">anthropic</SelectItem>
              <SelectItem value="openai">openai</SelectItem>
              <SelectItem value="google">google</SelectItem>
              <SelectItem value="custom">custom</SelectItem>
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
            placeholder="claude-sonnet-5"
          />
        </Field>
        <Field label="fallback（逗号分隔，格式 provider/name）" htmlFor="agent-fallback" helper="主模型不可用时按顺序降级">
          <Input
            id="agent-fallback"
            value={form.fallback}
            onChange={(e) => set('fallback', e.target.value)}
            className="h-12 rounded-sm"
            placeholder="openai/gpt-5, google/gemini-3-pro"
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

      <Section title="能力白名单">
        <Field label="tools" htmlFor="agent-tools">
          <ResourceMultiSelect types={['tool', 'mcp', 'knowledge_base']} selected={form.tools} onChange={(v) => set('tools', v)} />
        </Field>
        <Field label="skills" htmlFor="agent-skills">
          <ResourceMultiSelect types={['skill']} selected={form.skills} onChange={(v) => set('skills', v)} />
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

      {submitError && (
        <p role="alert" className="text-body-sm text-rust">
          {submitError}
        </p>
      )}

      <Button size="lg" disabled={pending} onClick={submit} className="self-start">
        {pending ? '保存中…' : '保存'}
      </Button>
    </div>
  )
}

export { EMPTY_FORM }
export type { FormState }
