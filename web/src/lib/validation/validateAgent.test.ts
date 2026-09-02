import { describe, expect, it } from 'vitest'

import { validateAgentDefinition } from '@/lib/validation/validateAgent'

const VALID_AGENT = {
  agent: 'architect',
  role: '系统架构师',
  version: '1.0',
  model: { provider: 'deepseek', name: 'deepseek-chat' },
  persona: '你是一名系统架构师',
  capabilities: { tools: [], skills: [] },
  constraints: { max_tokens_per_turn: 8000 },
}

describe('validateAgentDefinition', () => {
  it('accepts a well-formed definition', () => {
    expect(validateAgentDefinition(VALID_AGENT)).toEqual([])
  })

  it('flags a missing required field with a field path a form can map back', () => {
    const withoutPersona: Record<string, unknown> = { ...VALID_AGENT }
    delete withoutPersona.persona
    const errors = validateAgentDefinition(withoutPersona)
    expect(errors.some((e) => e.field === 'persona')).toBe(true)
  })

  // provider 不再是固定枚举：渠道由管理员在 系统配置 → 模型提供商 里创建，
  // schema 只能管住形状（小写字母开头的短标识）。"这个 provider 存不存在"
  // 是注册表的事。
  it('flags a malformed model.provider key at the nested field path', () => {
    const errors = validateAgentDefinition({ ...VALID_AGENT, model: { ...VALID_AGENT.model, provider: 'Not A Provider' } })
    expect(errors.some((e) => e.field === 'model.provider')).toBe(true)
  })

  it('rejects an agent ref that does not match the DSL pattern', () => {
    const errors = validateAgentDefinition({ ...VALID_AGENT, agent: 'Not-Valid!' })
    expect(errors.some((e) => e.field === 'agent')).toBe(true)
  })
})
