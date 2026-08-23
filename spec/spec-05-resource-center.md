# spec-05-resource-center 资源中心

> 对应任务 `task_implement.json#5` | 依赖：#4 auth
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

Tool / Skill / MCP Server / 知识库四类异构资源的统一注册与管理。

## 分表设计

四类资源**各自独立建表**：`tools` / `skills` / `mcp_servers` / `knowledge_bases`，不共用一张 `resources` 表。

**为什么不共用一张表**：四类资源的字段差异在设计阶段就已经明确——`mcp_servers` 需要 `health` 健康检查字段（连通性探测的结果），其余三类完全没有这个概念。共享一张表要么让 `health` 污染 tools/skills/knowledge_bases 的 schema（多数行永远是 `unknown`），要么在同一张表里为不同类型开不同的可空列，两种都比分表脏。四张表结构高度相似（除 `mcp_servers` 多一列），复制一份 migration 的成本很低，不构成"重复劳动"意义上的过度设计。

Agent 引用校验按 `ref` 查所属类型对应的表，不是四次查询——调用方本来就知道自己要校验的是 tool 还是 skill，直接查对应表即可，不需要每次校验都扫四张表。

## 关键实现

- `config` 中的凭证（API Key、Token）走 AES-256 加密存储，任何接口不回显
- MCP Server（`mcp_servers` 表）注册时做一次连通性探测，之后定时任务（5 分钟）刷新 `health` 字段；tools/skills/knowledge_bases 没有 `health` 字段
- 禁用资源不影响已有 Agent 的存量定义，但新建/编辑 Agent 时不可引用

## 验收清单

- [ ] 四种类型均可注册、列表、更新、启停
- [ ] 同一用户下同类型资源的 `(ref, version)` 唯一，重复返回 409 + 30003
- [ ] MCP 健康检查失败时 `health = unhealthy`，注册接口仍允许保存（便于事后修配置）
- [ ] 凭证字段在任何 GET 响应中都不出现
- [ ] 引用被禁用资源创建 Agent 时返回 30002
