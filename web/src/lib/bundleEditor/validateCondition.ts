/**
 * A lightweight structural check for the expr-lang condition strings edges
 * carry (`shared_state.tests_passed == true`) — not a full expr-lang
 * grammar (that's the backend's job at save time), just enough to catch
 * the errors a user actually makes while typing: unbalanced
 * parens/quotes, empty expression, an operator with a missing operand,
 * or a token that isn't `shared_state.*`, a literal, an operator, or a
 * boolean/comparison keyword. Real-time feedback in the properties
 * panel; the backend's graph validator remains the authority at save.
 */
export function validateCondition(expr: string): string | null {
  const trimmed = expr.trim()
  if (!trimmed) return '表达式不能为空'

  let depth = 0
  let inString: string | null = null
  for (const ch of trimmed) {
    if (inString) {
      if (ch === inString) inString = null
      continue
    }
    if (ch === '"' || ch === "'") {
      inString = ch
      continue
    }
    if (ch === '(') depth++
    if (ch === ')') depth--
    if (depth < 0) return '括号不匹配：多了一个 )'
  }
  if (inString) return `未闭合的字符串（缺少 ${inString}）`
  if (depth > 0) return '括号不匹配：缺少 )'

  if (!/shared_state\./.test(trimmed)) {
    return '表达式通常需要引用 shared_state.* 字段'
  }

  const trailingOperator = /(==|!=|>=|<=|&&|\|\||[+\-*/<>]|==)\s*$/
  if (trailingOperator.test(trimmed)) {
    return '表达式以运算符结尾，缺少右侧操作数'
  }

  const validTokenChars = /^[a-zA-Z0-9_.\s"'()!=<>&|+\-*/.]+$/
  if (!validTokenChars.test(trimmed)) {
    return '表达式包含不支持的字符'
  }

  return null
}
