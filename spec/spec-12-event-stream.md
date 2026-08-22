# spec-12-event-stream NDJSON 事件流接口

> 对应任务 `task_implement.json#12` | 依赖：#11 run_engine
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

实现 chunked HTTP + NDJSON 的事件流接口——**既不是 WebSocket 也不是 SSE**。

## 为什么是这个方案

| 对比项 | WebSocket | 轮询 | chunked NDJSON（采用） |
|---|---|---|---|
| 协议复杂度 | 需升级握手、连接状态机、心跳 | 无 | 无，就是普通 HTTP 响应 |
| 请求数 | 1 | 随时间线性增长 | 1 |
| 延迟 | 最低 | 轮询间隔 | 服务端检查间隔（300ms） |
| 双向 | 支持 | — | 不支持（本场景不需要，审批走 REST） |

单向推送场景下 chunked 是复杂度与效果的最佳平衡点，POC 已实测验证。

## 实现要点

```
Content-Type: application/x-ndjson
Transfer-Encoding: chunked
Cache-Control: no-cache
X-Accel-Buffering: no      ← 关键：不设置的话 nginx 会缓冲，流式变伪流式
```

- 建连后**先补发历史事件**追上当前进度，再进入 300ms ticker 增量循环
- 支持 `?after_id=N` 断线续传
- 遇 `bundle.finished` / `bundle.failed` 或客户端断开（`r.Context().Done()`）主动关闭
- 黑盒过滤条件是「请求者非作者」而非「存在 via_listing_id」——作者排查自己资源问题时需要完整事件流
- 建连阶段的错误用常规信封返回；连接后的错误推一条 `stream.error` 事件再关闭

## 多实例部署

引擎 goroutine 在实例 A，用户连接可能被路由到实例 B。V1 用**会话粘性**（同 run_id 路由到同实例）；实例故障时客户端重连 + `after_id` 补齐，机制已在 POC 验证。实例数上来后再评估共享事件总线（PostgreSQL LISTEN/NOTIFY）。

## 验收清单

- [ ] `curl -N` 观察到事件随执行进度陆续到达，不是攒完一次性返回
- [ ] 运行结束后连接在 ~400ms 内自动关闭
- [ ] `after_id` 续传不重复不丢失
- [ ] 经 nginx 代理后仍是真流式（验证 `proxy_buffering off` 生效）
- [ ] 黑盒运行：订阅者收不到 `is_internal` 事件，作者能收到
- [ ] 单次运行全流程客户端只发起 1 个 stream 请求
