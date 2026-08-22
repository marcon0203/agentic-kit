# spec-06-agent-center Agent 定义与版本管理

> 对应任务 `task_implement.json#6` | 依赖：#5 resource_center
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

Agent DSL 的定义、Schema 校验与版本管理。

## Agent DSL

完整 schema 见 `schemas/agent.schema.json`，字段结构见 `api/openapi.yaml` 的 `AgentDefinition`。

核心字段：`agent` / `role` / `version` / `model`(provider+fallback) / `persona` / `capabilities`(tools+skills+hooks) / `constraints` / `handoff`。

## 关键实现

### JSON Schema 校验而非 struct tag

用 `santhosh-tekuri/jsonschema/v5`，**同一份 schema 文件前后端复用**——前端表单实时校验、后端保存校验用的是同一份规则，不会出现"前端过了后端不过"。

校验失败时把 jsonschema 的错误路径转成 `details[].field`，前端据此在对应表单项标红。

### 版本化

- Bundle 引用**具体版本**而非"最新"，避免运行中的 Bundle 因 Agent 被改而行为漂移
- 已被订阅的版本 `immutable = true`，不可修改
- `definition` 与 `display_meta` 分离存储（黑盒基础，见 spec-08）

## 验收清单

- [ ] 非法 `model.provider` 返回 400 + 40001，`details` 指出具体字段与原因
- [ ] 引用不存在或已禁用的资源返回 30002
- [ ] 同 ref 可创建多个版本，版本列表按时间倒序
- [ ] 「从现有 Agent 复制」生成新 ref 且不影响原 Agent
- [ ] 修改已被订阅的版本返回 70005
