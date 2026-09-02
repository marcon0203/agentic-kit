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

  it('flags an invalid model.provider enum value at the nested field path', () => {
    const errors = validateAgentDefinition({ ...VALID_AGENT, model: { ...VALID_AGENT.model, provider: 'bogus' } })
    expect(errors.some((e) => e.field === 'model.provider')).toBe(true)
  })

  it('rejects an agent ref that does not match the DSL pattern', () => {
    const errors = validateAgentDefinition({ ...VALID_AGENT, agent: 'Not-Valid!' })
    expect(errors.some((e) => e.field === 'agent')).toBe(true)
  })
})
