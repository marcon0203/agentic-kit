# spec-10 ADK Go 2.0 集成与 DSL 编译层

> 对应任务 `task_implement.json#10` | 依赖：#7 bundle_center、#9 model_gateway
> 关联：架构文档「Bundle 编排引擎：ADK Go 2.0 集成方案」

## 目标

把平台的 Agent DSL / Bundle DSL 编译成 ADK Go 2.0 的图定义并执行。**平台不重复实现图调度、human-in-the-loop、状态持久化**——这些交给 ADK；平台只保留 ADK 不提供的产品层抽象（DSL、资源授权、事件契约、权限）。

## 分层边界

```
平台层（自研）：DSL 定义与校验、资源白名单授权、统一事件契约、权限与黑盒
      │
      │  ←── 本任务：编译层
      ▼
ADK Go 2.0（引擎，不改源码）：图调度、并行/条件、HITL、session 持久化、模型调用、OTel
```

**所有 ADK 调用收敛在 `internal/orchestrator/adk` 包内**，不散落到业务代码。ADK 从 1.x 到 2.0 有过破坏性变更，这层隔离是有价值的。

## 映射约定

| 平台 DSL | ADK |
|---|---|
| `agent` 定义（persona/model） | 一个 ADK Agent 节点 |
| `bundle.orchestration.edges` | 图的节点与边；`mode: parallel` → 并行分支 |
| `edge.condition` | 条件边，表达式用 `expr-lang/expr` 求值 |
| `human_gates[].after` | human-in-the-loop 确认节点，挂在对应节点后 |
| `shared_state` | ADK session state |
| `capabilities.tools/skills` | 编译期解析为 ADK Tool 实例 |
| `constraints` | ADK 节点级配置 + 平台层兜底熔断 |

## 实现要点

### 1. 条件表达式换成 expr-lang

POC 阶段的 `evalCondition` 只支持 `shared_state.key == value` 单一比较，生产环境换 `expr-lang/expr`：

```go
program, err := expr.Compile(edge.Condition, expr.Env(map[string]any{"shared_state": ...}), expr.AsBool())
```

编译在 Bundle 保存时做（提前暴露语法错误），不是运行时。

### 2. 资源授权在编译前完成

`capabilities.tools` 里的每个 ref 都要先经 Resource Registry 校验：存在、未禁用、当前用户有权使用。**未授权的资源不进入图**——不是运行时拦截，而是编译期就不构造这个 Tool。

### 3. 事件翻译层

ADK 产出自己的执行事件，平台订阅后翻译成 POC 已验证的事件模型（`node.started` / `node.thinking` / `node.tool_call.*` / `human_gate.*`）再落库。

**保留这层翻译而不是直接透传 ADK 事件**：前端契约不应该跟着上游框架的版本演进走。

翻译时同步标记 `is_internal`（`node.thinking` 等含内部推理的事件 → true），供黑盒过滤使用。

### 4. AgentRunner 适配边界

保留 POC 验证过的 `AgentRunner` 接口作为适配层，ADK 实现放在其后：

```go
type AgentRunner interface {
    Run(ctx context.Context, in AgentInput, emit func(Event)) (AgentOutput, error)
}
```

这样单测可以用 Mock 实现跑通编排逻辑，不必真的调模型。

## 验收清单

- [ ] `web-app-builder` 示例 Bundle 能编译成 ADK 图并完整执行到 END
- [ ] 并行分支（architect → [ui_designer, fullstack_engineer]）真正并发执行
- [ ] `wait_all` 汇聚正确等待两个上游都完成
- [ ] 条件边 `tests_passed == false` 触发自环重试，`== true` 走向 END
- [ ] 自环边不导致 join 死锁（POC 阶段踩过的坑，回归测试必须覆盖）
- [ ] human gate 暂停后，**重启进程**，运行仍能从暂停处恢复（ADK 持久化能力验证）
- [ ] 引用未授权资源的 Bundle，编译期即报错，不进入执行
- [ ] ADK 事件正确翻译为平台事件模型，`is_internal` 标记准确
- [ ] 所有 ADK 依赖仅出现在 `internal/orchestrator/adk` 包内（用 lint 或依赖检查工具约束）

## 风险

ADK Go 2.0 于 2026-06 GA，API 仍在快速演进。缓解：锁定次版本号，升级走独立评估；调用收敛在单一包内；保留 `AgentRunner` 抽象边界，必要时可替换实现。

## 已决事项

| 问题 | 结论 |
|---|---|
| ADK session state 是否支持 `shared_state` 的结构化校验 | **不依赖 ADK 做校验**。平台层在写入 session state 前用 JSON Schema 自行校验，ADK 只当存储用——这样不管 ADK 后续怎么改，校验规则都握在自己手里 |
| ADK 的 HITL 超时机制是否可配置 | **不依赖 ADK 的超时**。`human_gates[].timeout_seconds` 由平台侧定时任务扫描实现（见 spec-11），超时后主动向 ADK 注入结果。理由同上：超时策略是产品规则，不应受框架能力限制 |
