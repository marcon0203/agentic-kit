import { describe, expect, it } from 'vitest'

import { validateCondition } from '@/lib/bundleEditor/validateCondition'

describe('validateCondition', () => {
  it('accepts a well-formed comparison', () => {
    expect(validateCondition('shared_state.tests_passed == true')).toBeNull()
  })

  it('rejects an empty expression', () => {
    expect(validateCondition('  ')).toMatch(/不能为空/)
  })

  it('rejects unbalanced parens', () => {
    expect(validateCondition('(shared_state.x == 1')).toMatch(/括号/)
    expect(validateCondition('shared_state.x == 1)')).toMatch(/括号/)
  })

  it('rejects an unclosed string literal', () => {
    expect(validateCondition('shared_state.name == "abc')).toMatch(/未闭合/)
  })

  it('rejects a trailing operator', () => {
    expect(validateCondition('shared_state.x ==')).toMatch(/运算符/)
  })

  it('rejects an expression with no shared_state reference', () => {
    expect(validateCondition('1 == 1')).toMatch(/shared_state/)
  })
})
