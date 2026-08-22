# spec-11-run-engine 运行生命周期与 human gate

> 对应任务 `task_implement.json#11` | 依赖：#10 adk_integration、#8 marketplace
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

运行生命周期管理、human gate 审批、事件落库，以及黑盒资源的运行时安全边界。

## 运行创建链路

```
① 鉴权 → ② 订阅关系校验（未订阅返回 403+70003）
→ ③ 经 Marketplace 解析 listing → 订阅时绑定的那个版本（快照隔离）
→ ④ 依赖完整性复核（作者可能事后禁用了资源）
→ ⑤ 编译成 ADK 图并执行，标记 via_listing_id
→ ⑥ 事件落库时标记 is_internal
→ ⑦ Token 计入订阅者账户
```

**第 ② 步不能省**：只校验"listing 存在"的话，任何人拿到 ID 就能免订阅运行别人的资源。

**第 ④ 步错误必须脱敏**：直接回传"MCP Server internal-search 连接失败"等于泄露作者的内部资源名。

## human gate

- 审批接口做**审批人角色二次校验**（50004）——有查看权限 ≠ 有审批权限，这是安全边界
- 超时策略（`auto_approve` / `auto_reject` / `abort`）由平台侧定时任务扫描实现，超时后向 ADK 注入对应结果
- 审批记录额外落一份到 `audit_logs`（append-only）

## 黑盒 shared_state 过滤

黑盒运行时，共享状态只展示 `display_meta.io_description.outputs` 中声明的字段——中间节点可能往 shared_state 写了内部提示词片段，不过滤就泄露了。

## 全局熔断

`limits.max_total_tokens` / `max_cost_usd` / `max_wall_clock_seconds` 超限时终止运行并标记失败原因。

## 验收清单

- [ ] 未订阅用户运行返回 403 + 70003
- [ ] 运行使用的是订阅时绑定的版本，不是作者最新版本
- [ ] 依赖不可用时错误信息脱敏，不含内部资源名
- [ ] 无审批权限用户调用 gate 接口返回 403 + 50004
- [ ] 重复审批返回 409 + 50003
- [ ] gate 超时按配置策略正确处理
- [ ] 黑盒运行的 shared_state 只含声明的输出字段
- [ ] 超出 `max_cost_usd` 时运行被终止
- [ ] 审批记录写入 audit_logs 且不可篡改
