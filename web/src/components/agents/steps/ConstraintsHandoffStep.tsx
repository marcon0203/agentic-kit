import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { Field } from '@/components/agents/WizardField'
import type { FormState } from '@/lib/agents/definition'

interface ConstraintsHandoffStepProps {
  form: FormState
  set: <K extends keyof FormState>(key: K, value: FormState[K]) => void
}

export function ConstraintsHandoffStep({ form, set }: ConstraintsHandoffStepProps) {
  return (
    <div className="flex flex-col gap-space-8">
      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">记忆库</h3>
        <p className="text-caption text-ink-500">挂上记忆库后，还要在「技能与工具」的内置工具里勾选 load_memory 或 preload_memory，模型才会真的去检索它。</p>
        <ResourceMultiSelect
          types={['memory']}
          selected={form.tools}
          onChange={(refs) => set('tools', refs)}
          variant="card"
        />
      </section>

      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">执行约束</h3>
        <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2">
          <Field label="单轮最大 token" htmlFor="wizard-max-tokens">
            <Input
              id="wizard-max-tokens"
              value={form.maxTokensPerTurn}
              onChange={(e) => set('maxTokensPerTurn', e.target.value)}
              placeholder="4000"
            />
          </Field>
          <Field label="最大工具调用次数" htmlFor="wizard-max-tool-calls">
            <Input
              id="wizard-max-tool-calls"
              value={form.maxToolCalls}
              onChange={(e) => set('maxToolCalls', e.target.value)}
              placeholder="10"
            />
          </Field>
          <Field label="最大轮次" htmlFor="wizard-max-turns">
            <Input
              id="wizard-max-turns"
              value={form.maxTurns}
              onChange={(e) => set('maxTurns', e.target.value)}
              placeholder="6"
            />
          </Field>
          <Field label="超时（秒）" htmlFor="wizard-timeout">
            <Input
              id="wizard-timeout"
              value={form.timeoutSeconds}
              onChange={(e) => set('timeoutSeconds', e.target.value)}
              placeholder="120"
            />
          </Field>
        </div>

        <Field label="禁止的动作（逗号分隔）" htmlFor="wizard-forbidden">
          <Input
            id="wizard-forbidden"
            value={form.forbiddenActions}
            onChange={(e) => set('forbiddenActions', e.target.value)}
            placeholder="delete_production_data"
          />
        </Field>

        <Field label="输出 schema（可选）" htmlFor="wizard-output-schema" helper="填了就要求模型按这个结构回复">
          <Textarea
            id="wizard-output-schema"
            value={form.outputSchema}
            onChange={(e) => set('outputSchema', e.target.value)}
            rows={4}
            className="font-mono"
          />
        </Field>
      </section>

      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">协作契约</h3>
        <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2">
          <Field label="接受哪些节点的输入（逗号分隔）" htmlFor="wizard-accepts">
            <Input
              id="wizard-accepts"
              value={form.acceptsInputFrom}
              onChange={(e) => set('acceptsInputFrom', e.target.value)}
              placeholder="researcher"
            />
          </Field>
          <Field label="输出交给哪些节点（逗号分隔）" htmlFor="wizard-produces">
            <Input
              id="wizard-produces"
              value={form.producesOutputTo}
              onChange={(e) => set('producesOutputTo', e.target.value)}
              placeholder="reviewer"
            />
          </Field>
        </div>
        <label className="flex items-center gap-space-3">
          <Checkbox
            checked={form.requiresReview}
            onCheckedChange={(checked) => set('requiresReview', checked === true)}
          />
          <span className="text-body-sm text-ink-700">输出需要人工确认后才继续</span>
        </label>
      </section>
    </div>
  )
}
