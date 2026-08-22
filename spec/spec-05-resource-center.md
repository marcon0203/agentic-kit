# spec-05-resource-center 资源中心

> 对应任务 `task_implement.json#5` | 依赖：#4 auth
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

Tool / Skill / MCP Server / 知识库四类异构资源的统一注册与管理。

## 统一抽象

四类资源共用一张 `resources` 表，`type` 区分，差异化配置放 `config` JSONB。

**为什么不拆四张表**：V1 阶段共性大于差异，拆表会让 Agent 引用校验变成四次查询。真出现无法共享的字段再拆，不提前过度设计。

## 关键实现

- `config` 中的凭证（API Key、Token）走 AES-256 加密存储，任何接口不回显
- MCP Server 注册时做一次连通性探测，之后定时任务（5 分钟）刷新 `health` 字段
- 禁用资源不影响已有 Agent 的存量定义，但新建/编辑 Agent 时不可引用

## 验收清单

- [ ] 四种类型均可注册、列表、更新、启停
- [ ] 同一用户下 `(type, ref, version)` 唯一，重复返回 409 + 30003
- [ ] MCP 健康检查失败时 `health = unhealthy`，注册接口返回 30004 但仍允许保存（便于事后修配置）
- [ ] 凭证字段在任何 GET 响应中都不出现
- [ ] 引用被禁用资源创建 Agent 时返回 30002
