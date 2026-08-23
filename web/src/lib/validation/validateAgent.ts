import Ajv2020 from 'ajv/dist/2020'
import type { ErrorObject } from 'ajv'
import addFormats from 'ajv-formats'

import { agentSchema } from '@/lib/validation/agentSchema'

const ajv = new Ajv2020({ allErrors: true, strict: false })
addFormats(ajv)
const validate = ajv.compile(agentSchema)

export interface FieldError {
  field: string
  reason: string
}

/** Mirrors the backend's `details[].field` shape (dot/bracket path, no
 * leading slash) so a client-side error and a server-side 400's details
 * array can be rendered by the exact same code path. */
function toFieldPath(err: ErrorObject): string {
  const path = err.instancePath.replace(/^\//, '').replace(/\//g, '.').replace(/\.(\d+)/g, '[$1]')
  if (err.keyword === 'required') {
    const missing = (err.params as { missingProperty?: string }).missingProperty
    return path ? `${path}.${missing}` : (missing ?? path)
  }
  return path || '(root)'
}

/**
 * Validates an Agent definition against the exact same JSON Schema the
 * backend enforces (schemas/agent.schema.json) — spec-15: "前端过了后端不
 * 过" is what this exists to prevent. Returns [] when valid.
 */
export function validateAgentDefinition(definition: unknown): FieldError[] {
  const valid = validate(definition)
  if (valid) return []
  return (validate.errors ?? []).map((err) => ({
    field: toFieldPath(err),
    reason: err.message ?? '不符合 Schema',
  }))
}
