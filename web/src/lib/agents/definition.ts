import type { components } from '@/lib/api/schema'

type AgentDefinition = components['schemas']['AgentDefinition']
type ProviderName = components['schemas']['ProviderName']

/**
 * 表单态 ⇄ Agent DSL 的唯一一份转换。抽出来是因为现在有两个编辑器在共用
 * 它：分步的 AgentForm，和整屏的智能体工作台（AgentStudioPage）——两边要
 * 是各写一份，"表单里填的"和"存进去的"迟早会对不上。
 *
 * 每个字段都是字符串而不是目标类型：输入框里 "3." 这种中间态是合法的编辑
 * 过程，过早转成 number 会在用户还没打完的时候就把它吃掉。
 */
export interface FormState {
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
  builtinTools: string[]
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

export const EMPTY_FORM: FormState = {
  agent: '',
  role: '',
  version: '1.0',
  provider: 'deepseek',
  modelName: '',
  fallback: '',
  temperature: '',
  persona: '',
  tools: [],
  skills: [],
  builtinTools: [],
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

export const BUILTIN_TOOLS: { value: string; label: string; hint: string }[] = [
  { value: 'google_search', label: 'google_search', hint: '模型原生联网搜索（仅部分模型支持）' },
  { value: 'load_memory', label: 'load_memory', hint: '按需检索历史对话记忆' },
  { value: 'preload_memory', label: 'preload_memory', hint: '每轮自动注入相关历史记忆' },
  { value: 'load_artifacts', label: 'load_artifacts', hint: '读取本次会话已产出的附件' },
  { value: 'exit_loop', label: 'exit_loop', hint: '在循环编排中主动退出' },
]

export function csv(s: string): string[] {
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
    builtinTools: def.capabilities?.builtin_tools ?? [],
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

export function formStateToDefinition(f: FormState): AgentDefinition {
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
      builtin_tools: f.builtinTools as NonNullable<AgentDefinition['capabilities']>['builtin_tools'],
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
