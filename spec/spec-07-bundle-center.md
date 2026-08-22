# spec-07-bundle-center Bundle 编排定义与图校验

> 对应任务 `task_implement.json#7` | 依赖：#6 agent_center
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

Bundle 编排 DSL 的定义与**图静态校验**——把运行时才会暴露的编排错误提前到保存环节。

## Bundle DSL

见 `api/openapi.yaml` 的 `BundleDefinition`：`agents[]` 引用列表 + `orchestration`(mode/entry/edges/human_gates) + `limits`。

## 图校验规则（本任务的核心价值）

保存时必须校验，全部不通过则返回 422 + 40003：

1. `entry` 节点存在于 `agents[]` 声明中
2. 每条边的 `from` / `to` 都引用已声明的节点（或字面量 `END`）
3. **自环边不计入 join 前置计数** —— 这是 POC 阶段真实踩过的死锁 bug：
   `fullstack_engineer → fullstack_engineer` 的重试边如果被算进该节点自己的入边计数，
   这个节点会因为"要等自己先跑完"而永远无法首次触发。纯看 YAML 完全看不出来，
   必须在校验器里显式处理，并写回归测试。
4. 存在从 `entry` 出发不可达的节点 → 警告（不阻断，允许渐进式编辑）
5. `handoff` 声明与实际图连接不一致 → 警告（两份 DSL 可能由不同角色维护，允许暂时脱节但要可见）
6. `edge.condition` 表达式语法校验（用 expr-lang 预编译，提前暴露语法错误）

## 验收清单

- [ ] `entry` 不存在返回 422，指出具体字段
- [ ] 边引用未声明节点返回 422
- [ ] **自环重试边不触发死锁误报，且真实死锁能被检出**（回归测试必须覆盖这两个方向）
- [ ] 不可达节点产生警告但允许保存
- [ ] 非法条件表达式在保存时即报错，不留到运行时
- [ ] `web-app-builder` 示例 Bundle 校验通过
