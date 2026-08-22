# spec-09-model-gateway 模型网关

> 对应任务 `task_implement.json#9` | 依赖：#4 auth
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

统一模型 Provider 接入、调用抽象、降级链路与用量统计。

## 关键实现

### 凭证安全

`credentials` 字段 AES-256 加密存储，密钥走 K8s Secret 注入。**任何接口都不回显**，OpenAPI 中标 `writeOnly: true`。

### 降级链路

Agent DSL 的 `model.fallback` 是一个有序列表（`provider/name` 格式）。主模型失败时按顺序尝试，全部失败返回 60003。

降级发生时产出可观测事件，让用户在时间线里能看到"发生了降级"，而不是默默换了个模型。

### 用量统计

每次调用记录 token 数与成本，累加写入 `bundle_runs.total_tokens` / `cost_usd`，供 Chat 页右侧栏和运营中心报表使用。

**黑盒资源的用量算订阅者的**——是订阅者发起的运行、消耗的是订阅者配置的 Provider。

## 验收清单

- [ ] 三家 Provider（Anthropic/OpenAI/Google）均可接入并成功调用
- [ ] 凭证在任何 GET 响应与日志中都不出现
- [ ] 主模型不可用时按 fallback 顺序降级，时间线可见降级事件
- [ ] 全部降级失败返回 60003
- [ ] token 与成本准确累加到对应 run
