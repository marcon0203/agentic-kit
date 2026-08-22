# spec-08 应用广场：发布与订阅

> 对应任务 `task_implement.json#8` | 依赖：#7 bundle_center
> 关联：PRD「三点五、广场与订阅的核心约束」、架构文档「黑盒资源的运行时链路」

## 目标

让用户把自己的 Agent / Bundle / Skill / MCP 发布到广场，其他用户订阅后可直接使用。

两条**不可妥协**的规则贯穿本任务：

1. **黑盒**：订阅者能用不能看，任何接口/日志/错误信息都不得泄露 `definition`（persona、编排图、内部节点名、工具配置）
2. **快照隔离**：订阅绑定到具体版本，作者发新版本不影响存量订阅者，升级必须是订阅者的显式操作

## 数据模型

见架构文档第五章。本任务涉及 `marketplace_listings`、`subscriptions`，以及在 `agents`/`bundles`/`resources` 上使用 `display_meta` 与 `immutable` 字段。

关键点：`definition`（黑盒）与 `display_meta`（可公开）**分字段存储**，这是黑盒能安全实现的基础——不是"查出来再过滤"，而是"根本不查"。

## 接口

以 `api/openapi.yaml` 为准，本任务实现：

| 接口 | 要点 |
|---|---|
| `POST /marketplace/listings` | 依赖完整性校验，失败返回 422 + 70001 |
| `GET /marketplace/listings` | 只返回 `display_meta`，带 `subscribed` 标志 |
| `GET /marketplace/listings/{ref}` | 附 `constraints_summary`；已下架返回 404 + 70002 |
| `POST /marketplace/listings/{id}/subscribe` | 绑定具体版本 + 置 `immutable` |
| `GET /marketplace/subscriptions` | 带 `latest_version` / `latest_changelog` 提示 |
| `DELETE /marketplace/subscriptions/{id}` | 退订，历史运行记录保留 |
| `POST /marketplace/subscriptions/{id}/upgrade` | 显式升级 |

## 实现要点

### 1. 递归依赖闭包校验（发布环节，本任务最容易写错的地方）

规则：**一个资源要发布，它传递依赖的每一层都必须已发布**。不是只查直接引用那一层。

```
发布 Bundle web-app-builder v2.0
  └─ bundle.agents[] 里每个 Agent 的指定版本
       ├─ 该 Agent 是否已发布？          未发布 → 拒绝
       └─ 该 Agent 的 capabilities
            ├─ tools[]   每个是否已发布？ 未发布 → 拒绝
            ├─ skills[]  每个是否已发布？ 未发布 → 拒绝
            └─ hooks[]   引用的资源同上
```

**版本要精确匹配**：Bundle 引用的是 Agent 的**具体版本**，校验时要确认「Agent X **的那个版本**」已发布，而不是「Agent X 有任意版本发过」。作者很可能发布了 v1.0 却在 Bundle 里引用了未发布的 v1.1。

#### 实现要点

**用显式栈做深度优先遍历，不要写递归函数**——依赖深度理论上无上限，递归有爆栈风险，而且显式栈更容易在出错时输出完整路径。

**必须处理循环依赖**：Agent A 的 hook 引用了 Bundle B，而 Bundle B 又包含 Agent A——这在 DSL 层面是允许写出来的（hook 可以引用任意资源）。遍历时维护 `visiting` 集合，重复进入同一节点立即判定为循环依赖并返回专门的错误码，**不要让它转圈直到超时**。

**错误信息给完整路径**，不是只说"有依赖没发布"：

```json
{
  "code": 70001,
  "message": "存在未发布的依赖，无法发布",
  "details": [
    {
      "field": "agents[1].capabilities.tools[2]",
      "reason": "依赖路径 web-app-builder → architect@1.0 → mcp/internal-search@1.0 未发布"
    }
  ]
}
```

**一次返回全部问题**，不要发现第一个就中断——作者需要一次看到所有要处理的依赖，而不是修一个发一次、发现还有一个。

**性能**：单次发布的依赖节点通常在 20 个以内，但要避免 N+1 查询。先一次性把候选资源 ID 收集齐，再批量查发布状态。

### 2. 停止分发的连带约束

已发布资源若**正被其他已发布资源依赖**，不允许停止分发——否则上游资源的依赖闭包会断裂，订阅了上游的用户会突然跑不了。

停止分发前反查依赖方，存在时拒绝并列出具体是哪些资源在依赖它（错误码 70007）。作者需先处理上游，或保持该资源继续分发。

> 这条与「已被订阅的版本不可修改」是同一类保证：**已经对外承诺过的东西不能单方面撤回**。

### 3. DAO 层强制隔离

sqlc 生成两组**物理上不同**的查询方法，不靠业务层自觉过滤：

```sql
-- name: GetAgentForOwner :one
-- 返回全部字段，含 definition
SELECT * FROM agents WHERE id = $1 AND owner_user_id = $2;

-- name: GetAgentDisplayForSubscriber :one
-- 从 SELECT 列表里就没有 definition，泄露不了
SELECT id, agent_ref, version, display_meta, owner_user_id, created_at
FROM agents WHERE id = $1;
```

### 4. 订阅时置 immutable

订阅动作在同一事务内完成两件事：插入 `subscriptions` 行 + 将对应资源版本行 `immutable = true`。之后该行禁止 UPDATE/DELETE，**数据库触发器兜底**（不只靠应用层判断）。

### 5. 停止分发 vs 下架

| 动作 | 触发者 | 对存量订阅者的影响 |
|---|---|---|
| 停止分发（`distribution = 2`） | 作者 | 无影响，继续可用；广场不再显示 |
| 下架（`distribution = 3`） | 管理员（举报处理） | 收到通知，资源标记为不可用 |

## 验收清单

- [ ] 发布引用了未发布 MCP 的 Bundle，返回 422 + 70001
- [ ] **递归校验生效**：Bundle→Agent 已发布，但 Agent→MCP 未发布时，仍能被检出（不是只查一层）
- [ ] **版本精确匹配**：Agent v1.0 已发布但 Bundle 引用的是未发布的 v1.1 时，能检出
- [ ] 错误 `details` 给出完整依赖路径，而非只说"有依赖未发布"
- [ ] 存在多个未发布依赖时**一次性全部返回**，不是发现一个就中断
- [ ] 循环依赖能被检出并返回专门错误码，不会转圈到超时
- [ ] 被其他已发布资源依赖的资源，停止分发被拒绝（70007）并列出依赖方
- [ ] 广场列表与详情接口的响应中**不存在** `definition` 字段（写断言单测）
- [ ] 未订阅用户直接用 listing_id 请求运行，返回 403 + 70003
- [ ] 订阅 v1.0 后作者发布 v1.1，订阅者的 `subscribed_version` 仍为 v1.0
- [ ] 订阅者列表接口返回 `latest_version = 1.1` 且带 `latest_changelog`
- [ ] 尝试 UPDATE 已被订阅的资源版本行，数据库触发器拒绝
- [ ] 重复订阅同一 listing 返回 409 + 70004
- [ ] 退订后历史运行记录仍可查询
- [ ] 停止分发后，存量订阅者仍能正常运行

## 安全测试（必须写）

```go
// 黑盒泄露防护：遍历所有面向订阅者的接口，
// 断言响应 JSON 中不含 definition 相关的任何 key
func TestBlackboxNoDefinitionLeak(t *testing.T) { /* ... */ }

// 越权访问：未订阅用户不能通过任何路径拿到 definition
func TestUnsubscribedCannotAccess(t *testing.T) { /* ... */ }
```

## 已决事项

| 问题 | 结论 |
|---|---|
| 作者能否订阅自己的资源 | **不能**。直接用自己的即可；接口对 `author_user_id == subscriber_id` 返回 409 |
| 同一 `listing_ref` 能否同时订阅多个版本 | **不能**。一个 ref 只保留一个生效订阅，换版本走「升级」而非新增订阅 |
| 退订后重新订阅拿到哪个版本 | **当前最新版**，不恢复原来订阅的版本（退订即解除快照绑定） |
