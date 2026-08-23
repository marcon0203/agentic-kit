// Imports the exact JSON Schema the backend validates against (spec-15:
// "表单校验复用后端同一份 JSON Schema") — not a copy, the same file, so
// there's no drift to keep in sync.
import agentSchema from '../../../../schemas/agent.schema.json'

export { agentSchema }
