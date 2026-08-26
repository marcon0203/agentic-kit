import { ResourceMultiSelect } from '@/components/agents/ResourceMultiSelect'
import { PluginToolMultiSelect } from '@/components/agents/PluginToolMultiSelect'
import { CheckCardGroup } from '@/components/agents/CheckCardGroup'
import { BUILTIN_TOOLS } from '@/lib/agents/definition'
import type { FormState } from '@/lib/agents/definition'

interface SkillsToolsStepProps {
  form: FormState
  set: <K extends keyof FormState>(key: K, value: FormState[K]) => void
}

export function SkillsToolsStep({ form, set }: SkillsToolsStepProps) {
  return (
    <div className="flex flex-col gap-space-8">
      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">组件</h3>
        <p className="text-caption text-ink-500">HTTP 接口、OpenAPI 导入的操作、沙箱环境、MCP Server。需要先在资源中心注册。</p>
        <ResourceMultiSelect
          types={['tool', 'mcp']}
          selected={form.tools}
          onChange={(refs) => set('tools', refs)}
          variant="card"
        />
      </section>

      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">插件</h3>
        <p className="text-caption text-ink-500">已安装插件暴露的能力——包括可调用的工具和渲染器。</p>
        <PluginToolMultiSelect selected={form.tools} onChange={(refs) => set('tools', refs)} variant="card" />
      </section>

      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">Skill</h3>
        <p className="text-caption text-ink-500">一段打包好的固定做法，调用时把步骤交给模型照做。</p>
        <ResourceMultiSelect types={['skill']} selected={form.skills} onChange={(refs) => set('skills', refs)} variant="card" />
      </section>

      <section className="flex flex-col gap-space-3">
        <h3 className="text-label-md text-ink-900">内置工具</h3>
        <p className="text-caption text-ink-500">ADK 自带的实现，不需要在资源中心注册。</p>
        <CheckCardGroup
          options={BUILTIN_TOOLS.map((t) => ({
            value: t.value,
            label: t.label,
            helper: t.hint,
          }))}
          value={form.builtinTools}
          onChange={(refs) => set('builtinTools', refs)}
        />
      </section>
    </div>
  )
}
