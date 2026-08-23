# 灰度发布与回滚

对应 `task_implement.json#19` / spec-19 的验收项："灰度发布流程文档化，含回滚步骤"。

清单文件见 `deploy/k8s/`，按文件名前缀顺序 apply（00 命名空间 → 40 Ingress）。

## 前提

- `agentic-kit-server` 与 `agentic-kit-web` 两个 Deployment 各自独立扩缩容、独立发布 —
  server 是无状态 HTTP 服务（唯一的进程内状态是 spec-12 的事件流连接，靠 Ingress
  的 cookie 亲和性绑定到具体 Pod，不是靠共享存储），web 是纯静态资源，两者可以
  不同步发布。
- 数据库迁移**必须先于**新版本 server 上线执行（`20-migrate-job.yaml`），且
  `migrations/*.up.sql` 必须保持**加法优先**（新增列先允许 NULL 或给默认值、
  不在同一次发布内删除旧列/旧表）——这样回滚到上一个 server 镜像时，数据库
  schema 仍然兼容它。

## 灰度发布步骤（以 server 为例，web 同理）

1. **构建并推送新镜像**，打上不可变 tag（commit SHA，不用 `latest`）。
2. **跑迁移**：
   ```bash
   kubectl apply -f deploy/k8s/20-migrate-job.yaml
   kubectl wait --for=condition=complete job/agentic-kit-migrate -n agentic-kit --timeout=120s
   kubectl delete job/agentic-kit-migrate -n agentic-kit
   ```
3. **金丝雀**：先只切一小部分流量到新版本，观察真实请求而不是仅看探针。
   最简方式（无 service mesh 时）：临时把 `replicas` 拆成两个 Deployment
   （`agentic-kit-server` 保留旧镜像，新增 `agentic-kit-server-canary` 用新镜像，
   两者 `selector` 的 label 都匹配同一个 Service，Service 按 Pod 数量比例转发）：
   ```bash
   kubectl -n agentic-kit set image deployment/agentic-kit-server-canary \
     server=agentic-kit/server:<new-tag>
   kubectl -n agentic-kit scale deployment/agentic-kit-server-canary --replicas=1
   # 3 个旧 Pod + 1 个新 Pod ≈ 25% 流量在金丝雀上
   ```
   有 service mesh（Istio/Linkerd）或托管网关时，改用其原生的按权重分流，
   不必用双 Deployment 这种手动近似。
4. **观察**（建议至少 10-15 分钟，覆盖典型的运行周期）：
   - `/ready` 探针是否持续通过（数据库连通性）
   - 错误率：按 `code` 分组的 40001/50002 等业务错误码是否异常升高
   - OTel trace：抽查金丝雀 Pod 产生的 trace，确认 HTTP → 编排 → ADK → 模型调用
     链路完整、无异常长尾延迟
   - 日志中 `http_request` 事件的 `status`/`duration_ms` 分布
5. **确认无异常后，全量切换**：
   ```bash
   kubectl -n agentic-kit set image deployment/agentic-kit-server server=agentic-kit/server:<new-tag>
   kubectl -n agentic-kit rollout status deployment/agentic-kit-server
   kubectl -n agentic-kit scale deployment/agentic-kit-server-canary --replicas=0
   ```
   `maxUnavailable: 0`（见 `30-server-deployment.yaml`）保证滚动过程中可用副本数
   不低于 `replicas`，滚动全程不掉流量。

## 回滚

**触发条件**：金丝雀观察期或全量切换后出现错误率升高、`/ready` 持续失败、
或事件流连接异常中断。

```bash
kubectl -n agentic-kit rollout undo deployment/agentic-kit-server
kubectl -n agentic-kit rollout status deployment/agentic-kit-server
```

`rollout undo` 回退到上一个 ReplicaSet 记录的镜像 tag，同样走
`maxUnavailable: 0` 的滚动策略，不会有可用容量的空窗期。

如果本次发布还跑过迁移（第 2 步）且迁移不是纯加法（例如需要紧急处理的
线上问题被迫做了破坏性变更），必须先确认旧镜像仍能跑在新 schema
上再回滚代码；真正不兼容的 schema 变更需要单独的 `*.down.sql` 迁移
配合发布，而不是指望 `rollout undo` 一步解决——这种情况应在变更 review
阶段就避免，而不是留到回滚时处理。

web 的回滚是同一套命令，把 Deployment 名换成 `agentic-kit-web`；由于
web 没有数据库依赖，回滚永远是安全的。

## 事件流的会话粘性对灰度发布的影响

金丝雀期间，一个已经建立的事件流连接（`GET /runs/{id}/stream`）靠
`40-ingress.yaml` 的 `affinity: cookie` 粘在发起连接时选中的 Pod 上，
不会因为后续滚动更新把同一个流请求路由到不同 Pod。但 Pod 被滚动终止时
（`kubectl rollout` 逐个替换旧 Pod），该 Pod 上进行中的流会中断——
前端（spec-14）已经处理断线重连，重连请求会被亲和性 cookie 重新分配到
一个健康 Pod，从 `GET /runs/{id}` 拿到最新 `shared_state` 续上，不会
丢失已产生的运行结果（结果落库在 Postgres，不在进程内存）。
