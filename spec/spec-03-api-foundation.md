# spec-03-api-foundation API 基础设施（统一响应、中间件）

> 对应任务 `task_implement.json#3` | 依赖：#1 app_init
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

实现全站统一的响应格式、错误码体系和中间件链，后续所有接口复用。

## 统一响应信封

```go
type Envelope struct {
    Code      int            `json:"code"`
    Message   string         `json:"message"`
    Data      any            `json:"data"`
    RequestID string         `json:"request_id"`
    Details   []FieldError   `json:"details,omitempty"`
}
```

规范细节见架构文档 6.2 / 6.3。

## 中间件链（顺序重要）

```
recovery → request_id → logging → cors → auth → rate_limit → idempotency → handler
```

## 关键实现

### nil slice 防护（POC 阶段真实踩过的坑）

Go 的 `var out []T` 在无数据时序列化成 JSON `null`，前端 `list.map()` 直接抛异常白屏。

- 所有返回列表的结构体字段必须显式初始化为 `[]T{}`
- 写一个单测遍历全部列表接口，断言空结果返回 `[]` 而非 `null`
- 列入 Code Review 检查清单

### Idempotency-Key

创建类接口的 `Idempotency-Key` 请求头，24h 内相同 key 直接返回首次结果。存储用 PostgreSQL 表（key + 响应快照 + 过期时间）。

**为什么必须做**：一次 Bundle 运行可能消耗大量 Token，网络抖动导致的重复提交是真实的成本损失。

## 验收清单

- [ ] 所有响应（含错误）使用统一信封，204 除外
- [ ] 错误码常量表覆盖 10xxx~70xxx 全部区段
- [ ] `request_id` 与 OTel trace_id 一致，可在日志中串联
- [ ] 500 响应不泄露堆栈、SQL 或内部路径
- [ ] 限流触发返回 429 + 10002，带 `Retry-After` 头
- [ ] 相同 Idempotency-Key 重复请求不产生第二条记录
- [ ] 空列表接口返回 `[]`，有单测覆盖
