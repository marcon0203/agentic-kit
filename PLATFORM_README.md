# AI Agent 平台 — 设计与实施文档包

基于《AI Agent平台信息架构 V4》产出的完整设计文档与工程任务清单。

## 文件说明

```
.
├── docs/
│   ├── PRD_AI-Agent平台_AgentBundle编排与运行_V1.md   产品需求文档
│   └── 架构设计文档_AI-Agent平台_V1.md                 技术架构文档
├── api/
│   └── openapi.yaml                                    API 契约（唯一事实来源）
├── schemas/
│   ├── agent.schema.json                               Agent DSL 定义
│   ├── bundle.schema.json                              Bundle DSL 定义
│   ├── examples/                                       5 份可跑通的示例 DSL
│   └── README.md                                       Schema 说明与本地校验方法
├── spec/
│   └── spec-01 ~ spec-19 (19 份)                       各任务的实施规格
│                                                        （13~18 含 UI/UX设计 + UI验收标准）
├── design-system.md                                    全局设计系统（视觉规范唯一来源）
└── task_implement.json                                 工程任务清单
```

## 怎么用

1. **先读 PRD** 了解产品范围和用户故事
2. **再读架构文档** 了解技术栈、数据模型、API 规范、关键技术方案
3. **工程师按 `task_implement.json` 逐条执行**：
   - 每个任务的 `doc_dir` 指向对应的 spec 文档，里面有实现要点和验收清单
   - `depends_on` 标明前置依赖，按依赖顺序推进
   - 完成后把 `status` 从 `pending` 改为 `done`
4. **`api/openapi.yaml` 是前后端联调的唯一事实来源**，实现与规范不一致 CI 应当失败
5. **涉及界面的任务（#13~#18）先读 `design-system.md`**，再看该 spec 末尾的「UI/UX设计」与「UI验收标准」两节
6. **`schemas/` 下的两份 JSON Schema 前后端共用**：后端用 `santhosh-tekuri/jsonschema/v5` 校验，前端用 `ajv` 做表单实时校验，不要各自维护一份

## 关键设计决策速览

| 决策 | 选择 | 理由 |
|---|---|---|
| 编排引擎 | **Google ADK Go 2.0** | 图执行 + 内置 HITL + 崩溃恢复 + 原生 OTel，覆盖 POC 手写实现的全部短板 |
| 运行详情页 | **Chat 对话式** | 多 Agent 协作本质是团队讨论，对话流比仪表盘更直观；执行图退居辅助栏 |
| 事件推送 | **chunked HTTP + NDJSON** | 既不是 WebSocket（无需连接状态机）也不是 SSE；单向推送场景下复杂度与效果的平衡点 |
| 资源归属 | **用户级**（非团队级） | 支撑「个人创作 → 发布广场 → 他人订阅」的社区模型 |
| 广场可见性 | **黑盒** | 订阅者能用不能看，`definition` 与 `display_meta` 分字段存储 + DAO 层物理隔离 |
| 订阅版本策略 | **快照隔离** | 订阅绑定具体版本，作者发新版不影响存量订阅者，升级需显式操作 |
| 数据库 | PostgreSQL 16 | JSONB 存 DSL、原生分区应对事件表增长、LISTEN/NOTIFY 备用于多实例事件分发 |
| 视觉系统 | **PC 端浅色**（`tokens-pc.md`） | 后台控制台形态，只用 PC 端设计系统；V1 不做深色模式 |
| 登录形态 | **认证弹窗**（非独立路由） | 设计系统规定认证是 app shell 内的全局身份组件 |
| 节点状态色 | 复用既有语义色 | 待审批→warning、完成→success、失败→error，不新造平行色阶 |

## 从 POC 带过来的经验（已写进 spec 的验收清单）

本文档包的技术方案经过一轮可运行原型验证（Go 后端 + React 前端 + 端到端浏览器测试），过程中真实踩到并修复的问题已固化为规格要求：

- **自环边死锁**：Bundle 重试边若被计入自身 join 前置计数，节点永远无法首次触发。纯看 DSL 定义看不出来 → `spec-07` 的图校验规则 3 与回归测试
- **Go nil slice → JSON null**：空列表序列化成 `null` 导致前端 `.map()` 白屏，本地开发（库里有数据）不会触发，冷启动才暴露 → `spec-03` 的强制约定 + 单测
- **React StrictMode 双挂载竞态**：被 abort 的 effect 在 `finally` 里覆盖新实例的状态 → `spec-14` 的 `cancelled` 闭包保护
- **nginx 缓冲吃掉流式效果**：不关 `proxy_buffering`，chunked 响应会被攒着一次性转发 → `spec-19` 的 Ingress 配置

## 设计系统确立后修正的两处偏差

前期 POC 与架构文档在设计系统确立前编写，以下两处**以 `design-system.md` 为准**：

| 冲突项 | 早期写法 | 修正为 |
|---|---|---|
| 整体色调 | 深色控制台风格 | **浅色**：页面 `#f6f7fb`，卡片 `#ffffff` |
| 登录注册 | 独立路由页 + 路由守卫 | **弹窗形式**的全局身份组件，无独立路由 |

## 尚未确认的问题

见 PRD「八、待解决问题」，其中影响面较大的：

- 黑盒 Bundle 引用作者私有 MCP 时的处理策略（当前倾向发布前校验拦截）
- 多租户数据隔离是否需要在 V1 就做
- 模型成本的分摊口径（当前约定：黑盒运行的 Token 计订阅者）
