import { Textarea } from '@/components/ui/textarea'
import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { Field } from '@/components/agents/WizardField'
import { useFeatures } from '@/lib/features/useFeatures'
import type { FormState } from '@/lib/agents/definition'

interface PersonaKnowledgeStepProps {
  form: FormState
  set: <K extends keyof FormState>(key: K, value: FormState[K]) => void
}

export function PersonaKnowledgeStep({ form, set }: PersonaKnowledgeStepProps) {
  const { knowledgeBaseEnabled } = useFeatures()

  return (
    <div className="flex flex-col gap-space-6">
      <Field label="人设" htmlFor="wizard-persona" helper="描述 Agent 的角色定位、语气风格与行为准则">
        <Textarea
          id="wizard-persona"
          value={form.persona}
          onChange={(e) => set('persona', e.target.value)}
          placeholder="例如：你是一位耐心专业的客服代表，回答简洁，优先使用知识库内容。"
          rows={10}
          className="resize-y"
        />
      </Field>

      {knowledgeBaseEnabled ? (
        <Field label="知识库" htmlFor="wizard-kb" helper="被引用的知识库会作为可调用的检索工具进入能力白名单">
          <ResourceMultiSelect
            types={['knowledge_base']}
            selected={form.tools}
            onChange={(refs) => set('tools', refs)}
            variant="card"
          />
        </Field>
      ) : (
        <p className="text-body-sm text-ink-500">知识库功能未启用（KB_ENABLED），这台服务器上暂时不能给智能体挂知识库。</p>
      )}
    </div>
  )
}
