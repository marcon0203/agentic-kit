import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { FallbackMultiSelect } from '@/components/agents/FallbackMultiSelect'
import { TemperatureSlider } from '@/components/agents/TemperatureSlider'
import { Field } from '@/components/agents/WizardField'
import type { FormState } from '@/lib/agents/definition'
import { useProviderSpecs } from '@/lib/models/useProviderSpecs'
import type { components } from '@/lib/api/schema'

type ProviderName = components['schemas']['ProviderName']
type ModelCatalogEntry = components['schemas']['ModelCatalogEntry']

// 渠道由管理员在 系统配置 → 模型提供商 里创建，前端不硬编——一个部署有哪
// 些渠道只有后端知道。

interface BasicInfoStepProps {
  form: FormState
  set: <K extends keyof FormState>(key: K, value: FormState[K]) => void
  modelsForProvider: ModelCatalogEntry[]
  catalog: ModelCatalogEntry[]
  isEdit: boolean
}

export function BasicInfoStep({ form, set, modelsForProvider, catalog, isEdit }: BasicInfoStepProps) {
  const { specs } = useProviderSpecs()
  return (
    <div className="flex flex-col gap-space-6">
      <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2">
        <Field label="名称" htmlFor="wizard-role" helper="人类可读的角色名">
          <Input
            id="wizard-role"
            value={form.role}
            onChange={(e) => set('role', e.target.value)}
            placeholder="例如：客服助手"
          />
        </Field>
        <Field label="编码" htmlFor="wizard-agent" helper="唯一标识，编辑时不可修改">
          <Input
            id="wizard-agent"
            value={form.agent}
            onChange={(e) => set('agent', e.target.value)}
            placeholder="例如：cs-assistant"
            disabled={isEdit}
            className="font-mono"
          />
        </Field>
      </div>

      <Field label="模型厂商" htmlFor="wizard-provider">
        <Select value={form.provider} onValueChange={(v) => set('provider', v as ProviderName)}>
          <SelectTrigger id="wizard-provider" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {specs.map((spec) => (
              <SelectItem key={spec.name} value={spec.name}>
                {spec.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <div className="grid grid-cols-1 gap-space-6 md:grid-cols-2">
        <Field
          label="模型"
          htmlFor="wizard-model"
          helper={catalog.length > 0 && modelsForProvider.length === 0 ? '该厂商下还没有登记模型' : undefined}
        >
          <Select value={form.modelName} onValueChange={(v) => set('modelName', v)}>
            <SelectTrigger id="wizard-model" className="w-full">
              <SelectValue placeholder="选择模型" />
            </SelectTrigger>
            <SelectContent>
              {form.modelName && !modelsForProvider.some((m) => m.model === form.modelName) && (
                <SelectItem value={form.modelName}>{form.modelName}</SelectItem>
              )}
              {modelsForProvider.map((m) => (
                <SelectItem key={m.model} value={m.model}>
                  {m.display_name}（{m.model}）
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field label="Fallback 模型" htmlFor="wizard-fallback" helper="主模型不可用时按顺序尝试的备选">
          <FallbackMultiSelect
            catalog={catalog}
            value={form.fallback}
            onChange={(v) => set('fallback', v)}
            exclude={form.model ? `${form.provider}/${form.model}` : undefined}
          />
        </Field>
      </div>

      <Field label="Temperature" htmlFor="wizard-temperature" helper="控制输出随机性（0–2）">
        <TemperatureSlider value={form.temperature} onChange={(v) => set('temperature', v)} />
      </Field>
    </div>
  )
}
