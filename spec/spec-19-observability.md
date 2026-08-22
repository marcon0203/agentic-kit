# spec-19-observability 可观测性与部署

> 对应任务 `task_implement.json#19` | 依赖：#12 event_stream、#10 adk_integration
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

OpenTelemetry 接入、健康探针、Kubernetes 部署清单。

## 可观测性

- 接入 OTel Go SDK，`request_id` 与 trace_id 统一
- ADK Go 2.0 原生支持 OTel，接一个 TraceProvider 即可打通 Agent 执行链路的 span
- slog 结构化日志中带 trace_id，可与 trace 串联

## 部署要点

```yaml
# Ingress 关键配置
nginx.ingress.kubernetes.io/proxy-buffering: "off"        # 保证流式生效
nginx.ingress.kubernetes.io/affinity: "cookie"            # 事件流会话粘性
nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"    # 长连接不被中断
```

**`proxy-buffering: off` 是必须的**：不关缓冲，nginx 会把 chunked 响应攒起来一次性转发，"流式"变"伪流式"。

## 验收清单

- [ ] 一次 Bundle 运行在 trace 中可完整串联（HTTP → 编排 → ADK → 模型调用）
- [ ] `/health` 与 `/ready` 正确反映依赖状态（数据库连通性）
- [ ] 经 Ingress 访问事件流接口仍是真流式
- [ ] 事件流请求正确路由到同一实例（会话粘性生效）
- [ ] 灰度发布流程文档化，含回滚步骤
