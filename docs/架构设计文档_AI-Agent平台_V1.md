# AI Agent 平台 — 架构设计文档（V1）

**配套文档**：《产品需求文档：AI Agent 平台 — Agent Bundle 编排与运行（V1）》
**信息架构来源**：《AI Agent平台信息架构_V4》

---

## 一、系统概览

**系统定位**

一个让团队把模型 / Skill / 插件 / MCP / 知识库注册为资源，组合成具备角色约束的 Agent，再把多个 Agent 动态编排成 Bundle 协作执行任务的平台，核心模型 `Application -> Agent -> Model/Skill/Plugin/MCP/KnowledgeBase`。

**核心挑战**

- **挑战 1：多智能体编排的可靠性与可观测性**。并行分支、条件路由、human gate 审批，这些机制一旦实现错了很难从静态定义看出来——POC 阶段就真实踩过"自环边被误计入 join 前置条件导致死锁"这类问题。生产环境还要求 human gate 状态能扛住进程重启，不能因为服务重启就丢失一个等了半天的审批。
- **挑战 2：异构基础能力的统一注册与调用**。Tool、Skill、MCP Server、知识库四种性质不同的资源，要在资源中心用统一的抽象注册、在 Agent 定义里统一引用、在运行时统一以白名单方式授权给具体 Agent。
- **挑战 3：实时可视化对系统架构的连锁要求**。Web 端要低延迟看到执行图状态变化，这对事件产生、传输、消费的全链路都有要求——POC 阶段验证过 chunked HTTP 流式方案在单实例下的可行性，但生产环境多实例部署会引入新的复杂度（见六、关键技术方案）。
- **挑战 4：团队 / 权限模型下的资源与运行隔离粒度**。谁能定义 Agent、谁能编排 Bundle、谁能审批 human gate、谁能看到哪些团队的运行记录——这一层设计得太粗会有安全风险，设计得太细在 V1 阶段又是过度工程。

**设计原则**

- **DSL 声明式定义能力和编排，不硬编码**：Agent 能用哪些 tool/skill/hook、Bundle 怎么编排，都是数据（DSL + 数据库记录），不是写死在代码里的逻辑，保证可复用、可版本管理、未来可视化编辑
- **编排引擎与能力层解耦**：Bundle Orchestrator 只负责"按图调度"，不关心某个 Agent 具体怎么调模型、资源注册中心怎么存数据——这样能力层的实现细节变化不会影响编排逻辑
- **人工介入是一等公民，不是补丁**：human gate 在架构里和"节点执行完成"同等重要，要有持久化、要有权限控制、要有超时策略，不是简单的一个内存 channel
- **优先复用生产级框架，不重复造轮子**：编排引擎的核心机制（图执行、human-in-the-loop、状态恢复）由 **Google ADK Go 2.0** 承担，不把 POC 阶段的手写引擎搬进生产。平台自身只保留 POC 已验证的产品层抽象（Agent DSL / Bundle DSL / 事件流），这一层是 ADK 不提供的

**假设与约束**

- 【假设】初期服务企业内部团队，规模在数十到数百活跃用户量级，同时运行的 Bundle 并发数在几十量级，不是要支撑海量并发的对外 SaaS
- 【假设】技术栈：后端 Go（结合 ADK Go 2.0），前端 React + TypeScript，初期数据库 SQLite，多实例部署时升级 Postgres
- 【约束】Bundle 编排引擎**已确定采用 Google ADK Go 2.0**，本文档全部方案基于该前提展开。POC 阶段手写的 `Engine`/`Graph` 不进入生产，但其 `AgentRunner` 接口抽象保留，作为 ADK Runner 的适配层

---

## 二、整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                          Web 控制台（React）                        │
│  首页 │ 应用中心 │ 资源中心 │ 模型中心 │ 运营中心 │ 系统设置          │
└───────────────────────────┬──────────────────────────────────────┘
                             │ HTTPS / chunked HTTP 事件流
┌───────────────────────────▼──────────────────────────────────────┐
│                     BFF / API Gateway                              │
│           鉴权（JWT/API Key）、限流、路由到后端服务                  │
└───┬───────────┬────────────┬─────────────┬─────────────┬─────────┘
    │           │            │             │             │
┌───▼─────┐ ┌───▼──────────┐ ┌▼───────────┐ ┌▼───────────┐ ┌▼────────┐
│ Agent   │ │ Bundle        │ │ Resource   │ │ Model      │ │ IAM     │
│ Service │ │ Orchestrator  │ │ Registry   │ │ Gateway    │ │ Service │
│(应用中心 │ │（核心，基于   │ │（资源中心） │ │（模型中心） │ │(系统设置)│
│ Agent   │ │ ADK Go 2.0）  │ │            │ │            │ │         │
│ CRUD）  │ │               │ │            │ │            │ │         │
└───┬─────┘ └───┬───────────┘ └─────┬──────┘ └─────┬──────┘ └────┬────┘
    │           │                   │              │             │
    │      ┌────▼─────┐             │              │             │
    │      │Operation │◄────────────┴──────────────┴─────────────┘
    │      │ Service  │  （统一采集各服务的运行日志/调用日志/成本数据）
    │      │(运营中心) │
    │      └────┬─────┘
    │           │
┌───▼───────────▼──────────────────────────────────────────────────┐
│                          数据层                                     │
│  PostgreSQL（业务数据：agents/bundles/runs/resources/iam/审计）      │
│  对象存储（Bundle 产物、知识库文档）                                  │
│  向量库（知识库检索，可选 pgvector 起步）                             │
│  事件总线（多实例场景下的运行事件分发，见六、关键技术方案）             │
└──────────────────────────────────────────────────────────────────┘
                             │
┌───────────────────────────▼──────────────────────────────────────┐
│                      模型 Provider 层                                │
│         Anthropic / OpenAI / Google，经 Model Gateway 统一代理        │
└──────────────────────────────────────────────────────────────────┘
```

各层职责说明：

| 服务 | 对应 IA 导航 | 核心职责 |
|---|---|---|
| Agent Service | 应用中心 - 智能体管理 | Agent DSL 的 CRUD、JSON Schema 校验、版本管理 |
| Bundle Orchestrator | 应用中心 - 应用管理（Bundle 定义与运行） | Bundle DSL 校验、图构建、执行调度、human gate、事件产出——**系统的核心** |
| Resource Registry | 资源中心 | Tool / Skill / Plugin / MCP / 知识库的注册、发现、启停 |
| Model Gateway | 模型中心 | Provider 接入、模型路由与降级、token 统计 |
| Operation Service | 运营中心 | 汇总各服务的运行日志、调用日志、成本数据、审计日志 |
| IAM Service | 系统设置 | 用户、团队、权限、API Key 管理 |

---

## 三、技术栈选型

选型原则：优先成熟稳定、团队上手成本低、社区活跃的方案；不为了追新引入需要专人维护的组件；每一项都给出选择理由和被否掉的备选，方便后续复盘。

### 版本锁定原则

**所有依赖锁定到精确的补丁版本，不使用 `^` / `~` / `latest` / `x.y+` 这类浮动范围**。理由：本项目依赖 ADK Go 2.0 这种仍在快速演进的库（2026-06 才 GA，1.x→2.0 有过破坏性变更），浮动版本会让"昨天能跑今天挂了"变成常态，而且不同开发者机器上装出不同版本会产生难以复现的问题。

- Go：`go.mod` 写精确版本，提交 `go.sum`
- 前端：提交 `package-lock.json`，CI 用 `npm ci` 而非 `npm install`
- 容器镜像：用带 digest 的精确标签（`postgres:16.4-alpine@sha256:...`），不用 `latest`
- 升级走独立 PR，附变更影响评估，不夹带在功能 PR 里

下表的版本是**编写时的当前稳定版**，项目启动时应核对是否有安全补丁更新，确定后写死并在此表更新记录。

### 后端

| 分类 | 选型 | 锁定版本 | 理由 | 备选（未采用原因） |
|---|---|---|---|---|
| 语言 | Go | `1.24.0` | 并发模型天然适合多 Agent 并行编排；单二进制部署运维简单；POC 已验证 | Python（生态更丰富但并发和部署复杂度高）、Java（团队无明显优势） |
| Web 框架 | 标准库 `net/http` + `chi` | `github.com/go-chi/chi/v5 v5.1.0` | Go 1.22+ 标准库已支持 `GET /path/{id}` 路由模式，POC 阶段纯标准库已跑通；chi 只补充中间件链和路由分组，不引入框架式约束 | Gin（把 context 包了一层，与标准库生态互操作性差）、Echo（同上） |
| Agent 编排引擎 | **Google ADK Go** | `google.golang.org/adk v2.0.0` | **已定选型**。图执行引擎 + 内置 human-in-the-loop + 进程重启状态恢复 + 原生 OTel。**次版本号必须锁死**——2026-06 才 GA，API 仍在快速演进 | 手写引擎（POC 已验证但缺持久化/崩溃恢复）、Goa-AI（生产信号不足） |
| 模型 SDK | 各家官方 Go SDK | `anthropics/anthropic-sdk-go v1.13.0`、`openai/openai-go v1.8.0` | 官方 SDK 更新及时；经 Model Gateway 统一封装，保证 provider/fallback 可透明切换 | LiteLLM（Python 生态，需额外服务） |
| 数据访问 | `sqlc` + `pgx` | `sqlc v1.27.0`（工具）、`github.com/jackc/pgx/v5 v5.7.1` | sqlc 从 SQL 生成类型安全代码，SQL 完全可控（本项目有 JSONB 等复杂场景）；pgx 是 PostgreSQL 性能最好的原生驱动 | GORM（JSONB 支持弱，隐式行为多）、ent（schema DSL 学习成本高） |
| 数据库迁移 | `golang-migrate` | `github.com/golang-migrate/migrate/v4 v4.18.1` | 版本化 SQL 迁移，与 sqlc 配合自然 | Atlas（对本项目过重） |
| 表达式求值 | `expr-lang/expr` | `github.com/expr-lang/expr v1.16.9` | Bundle 条件边求值，替代 POC 的简易求值器 | cel-go（功能更强但语法对用户不友好） |
| 配置管理 | `viper` | `github.com/spf13/viper v1.19.0` | 配置文件 + 环境变量 + 默认值三级覆盖 | 纯环境变量（本地开发不便） |
| 日志 | `log/slog`（标准库） | Go 1.24 内置 | 标准库方案，无第三方依赖；与 OTel trace_id 关联方便 | zap（本项目日志量未到瓶颈）、logrus（已进入维护模式） |
| 可观测性 | OpenTelemetry Go SDK | `go.opentelemetry.io/otel v1.31.0` | trace/metrics 统一标准；ADK 原生支持 | 各自接 Prometheus + Jaeger（标准不统一） |
| JSON Schema 校验 | `santhosh-tekuri/jsonschema` | `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1` | POC 已验证：DSL 校验规则本身是数据，需前端复用，不能用 struct tag | `go-playground/validator`（只能校验 struct） |
| 测试 | `testify` + `testcontainers-go` | `stretchr/testify v1.9.0`、`testcontainers-go v0.34.0` | 起真实 PostgreSQL 跑集成测试 | sqlmock（掩盖真实 SQL 行为，POC 的 nil slice bug 就是这类问题） |

### 前端

| 分类 | 选型 | 锁定版本 | 理由 | 备选（未采用原因） |
|---|---|---|---|---|
| 框架 | React | `react 19.0.0` / `react-dom 19.0.0` | POC 已验证；团队熟悉度高 | Vue（团队无偏好优势） |
| 语言 | TypeScript | `typescript 5.7.2` | — | — |
| 构建 | Vite | `vite 7.0.0` | POC 已验证，冷启动和 HMR 快 | webpack（配置复杂） |
| 路由 | React Router | `react-router-dom 7.1.1` | POC 已验证 | TanStack Router（较新，收益不明显） |
| 服务端状态 | TanStack Query | `@tanstack/react-query 5.62.7` | 缓存/重试/失效策略开箱即用 | SWR（Query 生态更完整） |
| 客户端状态 | Zustand | `zustand 5.0.2` | 轻量，适合编排页这类局部复杂状态 | Redux Toolkit（样板代码多） |
| 样式 | Tailwind CSS | `tailwindcss 4.0.0` | 与 shadcn/ui 配套 | — |
| UI 组件 | **shadcn/ui（CLI 方式）** | CLI `shadcn@2.1.8`，组件源码进仓库 | 见下方「shadcn/ui 使用方式」 | Ant Design（定制成本高、包体大） |
| 图标 | lucide-react | `lucide-react 0.468.0` | shadcn/ui 默认图标库，线性描边风格与设计系统一致（`stroke-width: 1.8`） | — |
| 图编排可视化 | React Flow | `@xyflow/react 12.3.6` | Bundle 编排页拖拽连线 | 手写 SVG（拖拽编辑场景工作量大） |
| 图表 | Recharts | `recharts 2.15.0` | 运营中心成本报表 | ECharts（体积大，本项目图表简单） |
| Schema 校验 | Ajv | `ajv 8.17.1` | 前端复用后端同一份 JSON Schema 做表单实时校验 | zod（无法直接吃 JSON Schema 文件） |
| 事件流消费 | `fetch` + `ReadableStream` | 浏览器原生 | POC 已验证：chunked NDJSON 不需要任何库 | — |

### shadcn/ui 使用方式

**用 CLI 按需添加组件，不装运行时依赖包**——这是 shadcn/ui 的核心理念：组件源码直接进仓库，可以随意改，不受上游版本绑架。

```bash
# 初始化（生成 components.json + 基础配置）
npx shadcn@2.1.8 init

# 按需添加，只装用到的
npx shadcn@2.1.8 add button input card dialog tabs table badge
npx shadcn@2.1.8 add select checkbox textarea toast skeleton
```

约定：

| 项 | 约定 |
|---|---|
| 安装目录 | `src/components/ui/`（CLI 默认），**这些文件纳入版本控制** |
| 改动方式 | 直接改 `src/components/ui/` 下的源码，改动即固化；不要在业务代码里包一层去覆盖样式 |
| 与设计系统的关系 | `design-system.md` 的 token 写进 Tailwind config 和 CSS 变量，shadcn 组件消费这些变量——**不在组件里写死色值** |
| 升级 | 不执行全局重新 `add`（会覆盖本地改动）。需要上游修复时单独 diff 该组件，手动合并 |
| 新增组件 | 先查 shadcn 有没有，有就 CLI 添加后按设计系统调整；没有才自己写，并按 `component-spec.md` 补规范文档 |

平台自定义的四个组件（`ds-agent-bubble`、`ds-graph-node`、`ds-gate-card`、`ds-listing-card`）shadcn 没有对应物，放 `src/components/ds/`，与 `ui/` 目录分开。

### 基础设施

| 分类 | 选型 | 锁定版本 | 理由 |
|---|---|---|---|
| 主数据库 | PostgreSQL | `postgres:16.4-alpine`（镜像带 digest） | JSONB 存 DSL、原生分区应对事件表增长、`LISTEN/NOTIFY` 可用于多实例事件分发 |
| 向量库 | pgvector | `pgvector/pgvector:0.8.0-pg16` | 知识库检索量级不大时不引入独立组件，减少运维面 |
| 对象存储 | S3 兼容 | MinIO `RELEASE.2024-11-07T00-52-20Z` | Bundle 产物、知识库文档 |
| 缓存 | Redis（**V1 可选**） | `redis:7.4-alpine` | 仅用于 API 限流计数，不作为业务缓存 |
| 容器编排 | Kubernetes | `1.31` | 与部署架构一致 |
| CI/CD | GitHub Actions | Actions 用 commit SHA 固定，不用 `@v4` 浮动标签 | 防止第三方 action 被篡改 |

### 明确不在 V1 引入

以下组件在同类平台里常见，但本项目 V1 阶段**刻意不引入**，避免过度设计：

- **消息队列（Kafka/RabbitMQ）**：V1 的事件分发用 PostgreSQL `LISTEN/NOTIFY` 足够（见「七、关键技术方案」的事件流一节），引入 MQ 会增加一个需要专门维护的有状态组件
- **服务网格（Istio）**：服务数量个位数，K8s Service + Ingress 足够
- **独立的工作流引擎（Temporal）**：ADK Go 2.0 已内置状态恢复能力，不再需要额外引入 Temporal 及其集群运维成本
- **业务数据缓存**：见上表 Redis 一栏

---

## 四、模块划分与职责

```
[Bundle Orchestrator]  - Bundle 的图校验、执行调度、human gate、事件产出
  对外接口：POST /runs、GET /runs/{id}/stream、POST /runs/{id}/gate
  依赖：Agent Service（取 Agent 定义）、Model Gateway（实际模型调用）、
        Resource Registry（工具/skill 的实际执行）
  关键设计：基于 ADK Go 2.0 的图执行引擎；shared_state 作为跨节点的
           结构化上下文，避免完整对话历史随节点数线性膨胀
  数据所有权：bundles、bundle_runs、bundle_run_events

[Agent Service]  - Agent DSL 的定义与生命周期管理
  对外接口：Agent 的增删改查、DSL 校验
  依赖：Resource Registry（校验 capabilities 引用的资源是否存在且未禁用）
  关键设计：Agent 定义支持版本化，Bundle 引用具体版本而非"最新"，
           避免正在运行的 Bundle 因为 Agent 被改动而行为漂移
  数据所有权：agents、agent_versions

[Resource Registry]  - 异构基础能力的统一注册与发现
  对外接口：注册/查询/启停 Tool、Skill、MCP Server、知识库
  依赖：无（平台的能力底座）
  关键设计：四类资源统一抽象成 capability descriptor（id、类型、
           连接配置、健康状态），Agent 只需要按 id 引用
  数据所有权：tools、skills、mcp_servers、knowledge_bases

[Model Gateway]  - 模型 Provider 的统一接入与路由
  对外接口：Provider 的增删改查，供 Bundle Orchestrator 内部调用的
           统一模型调用接口
  依赖：无
  关键设计：Agent DSL 里的 model.provider + fallback 列表在这里被
           实际解析成调用链，主模型失败按顺序降级
  数据所有权：model_providers、model_call_logs

[Operation Service]  - 可观测性与审计
  对外接口：运行列表、成本报表、审计日志查询
  依赖：订阅其他服务产出的事件/日志（不直接读其他服务的业务表）
  关键设计：只做聚合和展示，不承担业务逻辑，保证其他服务可以独立演进
  数据所有权：audit_logs、cost_records（业务事件的聚合视图）

[Marketplace Service]  - 广场发布、订阅关系、黑盒边界的守门人
  对外接口：广场浏览/搜索、发布、订阅/退订、版本升级、举报
  依赖：Agent Service / Bundle Service（读资源定义做发布校验）、IAM Service
  关键设计：**黑盒边界在这一层收口**——所有面向订阅者的资源读取都必须经过
           本服务，由它决定返回 display_meta 还是完整 definition；发布前做
           依赖完整性校验，禁止发布引用了作者私有资源的内容
  数据所有权：marketplace_listings、subscriptions、reports

[IAM Service]  - 身份与权限
  对外接口：用户/团队 CRUD、鉴权、权限校验
  依赖：无
  关键设计：权限模型覆盖查看/编辑/运行/审批四种粒度，human gate 的
           审批人可限定为特定角色
  数据所有权：users、teams、roles、permissions、api_keys
```

模块依赖关系图（文字版）：

```
Bundle Orchestrator ──依赖──▶ Agent Service ──依赖──▶ Resource Registry
        │                          ▲                            ▲
        │                          │                            │
        ├──依赖──▶ Model Gateway   │                            │
        │                          │                            │
        └──依赖──▶ Marketplace Service ──────────────────────────┘
                   （订阅资源的解析与黑盒过滤）

所有服务 ──鉴权依赖──▶ IAM Service
所有服务 ──事件/日志上报──▶ Operation Service
```

---

## 五、数据库设计（PostgreSQL）

### 归属模型

资源归属从"团队"改为"**用户**"：每个用户拥有独立的资源空间，团队作为可选的协作单位存在（V1 可以先只有个人空间）。所有业务表带 `owner_user_id`，权限判断的默认规则是"只能访问自己的资源 + 自己订阅的资源"。

### 核心表结构

```sql
-- ── 用户体系 ──────────────────────────────────────────────
CREATE TABLE users (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    email           VARCHAR(255)  NOT NULL,
    password_hash   VARCHAR(255)  NOT NULL,  -- argon2id
    display_name    VARCHAR(64)   NOT NULL,
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (email)
);

-- ── 资源定义（归属到用户，版本化） ─────────────────────────
CREATE TABLE agents (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    agent_ref       VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL,
    definition      JSONB         NOT NULL,  -- 完整 Agent DSL，含 persona 等黑盒内容
    display_meta    JSONB         NOT NULL DEFAULT '{}',
        -- 可公开展示的元信息：display_name/description/usage/io_description
        -- 与 definition 分离存储，是黑盒发布能安全实现的基础
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
        -- 一旦该版本被订阅过即置为 true，之后不可修改/删除（快照隔离的保证）
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, agent_ref, version)
);

CREATE TABLE bundles (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    bundle_ref      VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL,
    definition      JSONB         NOT NULL,  -- 含编排图等黑盒内容
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, bundle_ref, version)
);

CREATE TABLE resources (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    type            VARCHAR(16)   NOT NULL,  -- tool/skill/mcp/knowledge_base
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,  -- 连接配置，含凭证引用，黑盒内容
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, type, ref, version)
);

-- ── 广场发布与订阅 ────────────────────────────────────────
CREATE TABLE marketplace_listings (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    author_user_id  BIGINT        NOT NULL REFERENCES users(id),
    resource_type   VARCHAR(16)   NOT NULL,  -- agent/bundle/skill/mcp
    resource_id     BIGINT        NOT NULL,  -- 指向上述表的具体版本行
    listing_ref     VARCHAR(64)   NOT NULL,  -- 广场内的稳定标识（跨版本不变）
    version         VARCHAR(16)   NOT NULL,
    visibility      VARCHAR(16)   NOT NULL DEFAULT 'blackbox',
        -- V1 仅 blackbox；预留 public 供 P2-4 的"公开可复制"模式
    changelog       TEXT,         -- 作者手写的更新说明（不是 diff，diff 会泄露黑盒内容）
    distribution    SMALLINT      NOT NULL DEFAULT 1,
        -- 1 分发中 / 2 已停止分发（不影响存量订阅）/ 3 已下架（举报处理）
    subscriber_count INTEGER      NOT NULL DEFAULT 0,  -- 冗余计数，避免列表页 count(*)
    run_count       BIGINT        NOT NULL DEFAULT 0,
    published_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (listing_ref, version)
);

CREATE TABLE subscriptions (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    subscriber_id   BIGINT        NOT NULL REFERENCES users(id),
    listing_id      BIGINT        NOT NULL REFERENCES marketplace_listings(id),
        -- 关键：绑定到具体版本的 listing，而非 listing_ref
        -- 这就是"快照隔离"在数据模型上的落点
    local_alias     VARCHAR(64),  -- 订阅者可在自己空间内重命名
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (subscriber_id, listing_id)
);

-- ── 运行时 ────────────────────────────────────────────────
CREATE TABLE bundle_runs (
    id              VARCHAR(32)   PRIMARY KEY,
    bundle_id       BIGINT        NOT NULL REFERENCES bundles(id),
    triggered_by    BIGINT        NOT NULL REFERENCES users(id),
    via_listing_id  BIGINT        REFERENCES marketplace_listings(id),
        -- 非空表示这次运行来自订阅的黑盒资源，事件流和执行图需按黑盒规则脱敏
    status          VARCHAR(16)   NOT NULL,
    error           TEXT,
    shared_state    JSONB         NOT NULL DEFAULT '{}',
    total_tokens    BIGINT        NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(10,4) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ
);

CREATE TABLE bundle_run_events (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    run_id          VARCHAR(32)   NOT NULL REFERENCES bundle_runs(id),
    type            VARCHAR(32)   NOT NULL,
    node            VARCHAR(64),
    payload         JSONB,
    is_internal     BOOLEAN       NOT NULL DEFAULT false,
        -- 标记该事件是否包含黑盒内部信息（如 node.thinking 的推理过程）
        -- 黑盒运行时，is_internal = true 的事件不推送给订阅者，但仍落库供作者排查
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE model_providers (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    provider        VARCHAR(16)   NOT NULL,
    credentials     BYTEA         NOT NULL,  -- AES-256 加密
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);
```

### 黑盒发布的实现要点

黑盒能安全实现，靠的是三个设计配合，缺一不可：

1. **`definition` 与 `display_meta` 分表字段存储**。对外接口按用户身份选择返回哪个字段——是作者返回全部，是订阅者只返回 `display_meta`。这比"返回后再过滤"更可靠，因为过滤逻辑一旦漏一处就是泄露。
2. **DAO 层强制隔离**。查询订阅资源的方法从设计上就不 SELECT `definition` 字段（sqlc 生成两组不同的查询方法：`GetAgentForOwner` / `GetAgentDisplayForSubscriber`），而不是靠业务层自觉。
3. **事件流按 `is_internal` 过滤**。`node.thinking` 这类会暴露内部推理的事件标记为内部事件，黑盒运行时不推给订阅者。注意事件仍然要落库——作者需要它来排查自己资源的问题。

### 快照隔离的实现要点

- 订阅动作发生时，把对应资源版本行的 `immutable` 置为 `true`，此后该行禁止 UPDATE / DELETE（数据库层可加触发器兜底，不只靠应用层判断）
- 作者"发布新版本"是插入新行（新 version），不是更新旧行
- 运行时用 `subscriptions.listing_id → marketplace_listings.resource_id` 精确定位到订阅时的那一行定义，永远不查"最新版本"
- **存储成本**：被订阅过的历史版本永久保留，V1 阶段量级可接受；后续可对"零订阅且已停止分发"的旧版本做归档

### 索引策略

| 表 | 索引 | 使用场景 |
|---|---|---|
| bundle_runs | idx_bundle_status | 运营中心按 Bundle + 状态筛选运行列表 |
| bundle_run_events | idx_run_id (run_id, id) | 事件流按游标增量查询（`id > N`），是 chunked 流式接口的查询热点 |
| agents / bundles / resources | UNIQUE(owner_user_id, ref, version) | 保证同一用户空间内 ref+version 唯一，支持版本化引用 |
| marketplace_listings | idx_type_distribution (resource_type, distribution, published_at DESC) | 广场列表页按类型筛选 + 按发布时间排序的主查询路径 |
| subscriptions | UNIQUE(subscriber_id, listing_id) | 防重复订阅；同时是"我的订阅"列表的查询入口 |
| bundle_runs | idx_triggered_by (triggered_by, created_at DESC) | 用户查看自己的运行历史 |

### 分片策略

【假设】初期不分片。`bundle_run_events` 是增长最快的表，量级上来后优先考虑按 `run_id` 或时间做分区表（PostgreSQL 原生分区），而不是应用层分片。

---

## 六、API 设计规范

本章是前后端联调的契约，所有接口必须遵守，不允许各服务自行定义响应格式。

### 6.1 通用约定

| 项 | 约定 |
|---|---|
| 基础路径 | `/api/v1`，版本号进 URL；破坏性变更升 `v2`，旧版本至少并行 3 个月 |
| 传输 | 仅 HTTPS；请求/响应体统一 `application/json; charset=utf-8`（事件流接口除外） |
| 命名 | URL 路径用小写中划线（`/model-providers`），JSON 字段用小写下划线（`bundle_id`），保持前后端一致 |
| 时间 | 一律 RFC3339 带时区（`2026-08-20T13:10:00Z`），不用 Unix 时间戳，不用本地时间 |
| ID | 对外暴露的资源 ID 用字符串（`"run-9e67931d5c38024e"`、`"1234567890"`），避免 JS `Number` 精度丢失导致大整数 ID 被截断 |
| 空数组 | 列表字段无数据时必须返回 `[]`，**不允许返回 `null`** |

> **最后一条是硬性要求，不是风格偏好。** POC 阶段真实踩过这个坑：Go 的 `var out []T` 在没有数据时序列化成 JSON `null`，前端 `list.map()` 直接抛异常白屏。所有返回列表的结构体字段必须显式初始化为 `[]T{}`，此项列入 Code Review 检查清单。

### 6.2 统一响应结构

**所有** JSON 接口（含错误）使用同一层信封，前端只需写一处解包逻辑：

```jsonc
// 成功
{
  "code": 0,                    // 固定 0 表示成功
  "message": "ok",
  "data": { /* 业务数据，无返回内容时为 null */ },
  "request_id": "req-a1b2c3d4"  // 全链路追踪 ID，等同 OTel trace_id
}

// 失败
{
  "code": 40001,                          // 业务错误码，非 0
  "message": "引用的资源已被禁用",           // 面向用户的可读文案，可直接展示
  "data": null,
  "request_id": "req-a1b2c3d4",
  "details": [                            // 可选，字段级错误，表单场景使用
    { "field": "capabilities.tools[2]", "reason": "资源 mcp/internal-search 已被禁用" }
  ]
}
```

**HTTP 状态码与业务码并用**，各司其职：HTTP 状态码给网关、监控、客户端 SDK 做粗粒度判断；业务 `code` 给前端做精确分支。

| HTTP | 使用场景 |
|---|---|
| 200 | 查询成功、更新成功 |
| 201 | 创建成功（响应体带新资源 ID） |
| 204 | 成功但无响应体（如 human gate 审批）——**注意：204 不带信封，响应体为空** |
| 400 | 请求参数格式错误、DSL Schema 校验失败 |
| 401 | 未认证（Token 缺失/过期） |
| 403 | 已认证但无权限 |
| 404 | 资源不存在 |
| 409 | 状态冲突（如 human gate 已被处理、版本号重复） |
| 422 | 参数格式合法但业务规则不通过（如 Bundle 图存在死锁节点） |
| 429 | 触发限流 |
| 500 | 服务端未预期异常（`message` 不得暴露堆栈或 SQL） |
| 503 | 依赖不可用（模型 Provider 全部降级失败） |

### 6.3 错误码规范

五位数字，`AABBB`：前两位 = 模块，后三位 = 该模块内序号。

| 区段 | 模块 | 示例 |
|---|---|---|
| 10xxx | 通用/网关 | `10001` 参数校验失败、`10002` 限流、`10003` 请求体过大 |
| 20xxx | IAM | `20001` Token 无效、`20002` Token 过期、`20003` 无权限、`20004` API Key 已吊销 |
| 30xxx | 资源中心 | `30001` 资源不存在、`30002` 资源已禁用、`30003` ref 重复、`30004` MCP Server 健康检查失败 |
| 40xxx | 应用中心（Agent/Bundle） | `40001` Agent DSL 不合法、`40002` Bundle DSL 不合法、`40003` 图校验失败（含死锁边）、`40004` 引用的 Agent 版本不存在 |
| 50xxx | 编排运行时 | `50001` Run 不存在、`50002` Run 已结束、`50003` human gate 已被处理、`50004` 非指定审批角色、`50005` Bundle 执行超时 |
| 60xxx | 模型中心 | `60001` Provider 未配置、`60002` 凭证无效、`60003` 主模型与全部降级模型均不可用、`60004` 超出 Token 配额 |
| 70xxx | 广场与订阅 | `70001` 发布失败：存在未发布的依赖、`70002` listing 不存在或已下架、`70003` 未订阅该资源、`70004` 重复订阅、`70005` 该版本已被订阅不可修改、`70006` 黑盒资源不允许查看定义、`70007` 被其他已发布资源依赖，不可停止分发、`70008` 依赖闭包存在循环引用 |

约定：错误码一旦发布**只增不改**，废弃的码位保留不复用；`message` 文案可以改进，但前端分支逻辑只依赖 `code`，不得依赖文案匹配。

### 6.4 分页、排序、过滤

列表接口统一用游标分页（`bundle_run_events` 这类高频增长表用偏移分页会有翻页错位和深翻性能问题）：

```jsonc
// GET /api/v1/runs?limit=20&cursor=eyJpZCI6MTIzfQ&status=failed&sort=-created_at
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [ /* ... */ ],          // 无数据时为 []，不是 null
    "next_cursor": "eyJpZCI6MTQzfQ", // 无更多数据时为 null
    "has_more": true
  },
  "request_id": "req-a1b2c3d4"
}
```

- `limit`：默认 20，上限 100，超出按上限截断而非报错
- `sort`：字段名前加 `-` 表示倒序，多字段用逗号分隔
- 过滤参数直接用字段名做 query key（`status=failed`），多值用逗号分隔

### 6.5 认证与鉴权

| 方式 | 使用场景 | Header |
|---|---|---|
| JWT Bearer | Web 控制台用户会话，有效期 2h，配 refresh token（7d） | `Authorization: Bearer <token>` |
| API Key | 服务端调用、CI 集成 | `Authorization: ApiKey <key>` |

鉴权分三层：

1. **网关层**：校验身份合法性（401）
2. **业务层**：校验数据权限（403）。**human gate 审批必须在业务层做二次角色校验**——有 Bundle 查看权限不等于有审批权限
3. **黑盒边界层**：面向订阅者的资源读取一律经 Marketplace Service 收口，由它决定返回 `display_meta` 还是完整 `definition`。DAO 层提供两组独立的查询方法（`GetXxxForOwner` / `GetXxxDisplayForSubscriber`），从设计上就不给"忘记过滤"留机会

### 6.6 幂等性

所有 `POST` 创建类接口支持 `Idempotency-Key` 请求头（客户端生成 UUID）：

- 相同 Key 在 24h 内重复请求，直接返回首次的结果，不重复创建
- 典型场景：网络抖动导致前端重试 `POST /runs`，不应产生两次 Bundle 运行（一次运行可能消耗大量 Token，重复执行是真实的成本损失）

### 6.7 事件流接口（特例说明）

`GET /api/v1/runs/{id}/stream` 是**唯一不遵守统一信封**的接口，因为它是流式响应而非单次 JSON：

```
Content-Type: application/x-ndjson
Transfer-Encoding: chunked
Cache-Control: no-cache
X-Accel-Buffering: no          # 关键：告知 nginx 不要缓冲，否则流式变伪流式
```

每行一个独立 JSON 事件对象（POC 已验证的结构）：

```jsonc
{"id":42,"type":"node.started","run_id":"run-xxx","node":"architect","timestamp":"2026-08-20T13:10:00Z","payload":null}
```

- 建连后先补发历史事件（客户端可带 `?after_id=N` 断线续传），再进入增量推送
- 服务端在 `bundle.finished` / `bundle.failed` 后主动关闭连接
- 鉴权失败等错误在**建连阶段**用常规信封 + 对应 HTTP 状态码返回；连接建立后发生的错误，以一条 `type: "stream.error"` 的事件推送后关闭

### 6.8 核心接口清单

完整接口在 OpenAPI 3.1 规范文件中定义（`api/openapi.yaml`，作为前后端联调的唯一事实来源，CI 中校验实现与规范一致）。此处列核心接口：

| 方法 | 路径 | 说明 | 主要错误码 |
|---|---|---|---|
| POST | `/api/v1/resources` | 注册 Tool/Skill/MCP/知识库 | 30003, 30004 |
| GET | `/api/v1/resources` | 资源列表（支持 `type` 过滤） | — |
| POST | `/api/v1/agents` | 创建 Agent（含 DSL Schema 校验） | 40001, 30002 |
| GET | `/api/v1/agents/{ref}/versions` | Agent 版本列表 | 404 |
| POST | `/api/v1/bundles` | 创建 Bundle（含图校验） | 40002, 40003, 40004 |
| POST | `/api/v1/runs` | 启动一次运行（支持 `Idempotency-Key`） | 30002, 40004, 60001 |
| GET | `/api/v1/runs` | 运行列表（游标分页） | — |
| GET | `/api/v1/runs/{id}` | 运行详情 | 50001 |
| GET | `/api/v1/runs/{id}/stream` | 事件流（NDJSON，见 6.7） | 50001 |
| POST | `/api/v1/runs/{id}/gate` | human gate 审批（204） | 50002, 50003, 50004 |
| POST | `/api/v1/model-providers` | 接入模型 Provider | 60002 |
| POST | `/api/v1/auth/register` | 注册 | 10001 |
| POST | `/api/v1/auth/login` | 登录，返回 JWT + refresh token | 20001 |
| GET | `/api/v1/marketplace/listings` | 广场浏览（支持类型筛选、搜索） | — |
| GET | `/api/v1/marketplace/listings/{ref}` | 广场资源详情（黑盒：只返回 display_meta） | 70002 |
| POST | `/api/v1/marketplace/listings` | 发布资源到广场（含依赖完整性校验） | 70001 |
| POST | `/api/v1/marketplace/listings/{id}/subscribe` | 订阅（绑定到该具体版本） | 70002, 70004 |
| DELETE | `/api/v1/marketplace/subscriptions/{id}` | 退订 | 404 |
| GET | `/api/v1/marketplace/subscriptions` | 我的订阅（含"有新版本"提示） | — |
| POST | `/api/v1/marketplace/subscriptions/{id}/upgrade` | 升级到指定新版本（显式操作） | 70002 |

请求/响应示例（`POST /api/v1/runs`）：

```jsonc
// Request
// Headers: Authorization: Bearer <token>, Idempotency-Key: 550e8400-...
{
  "bundle_ref": "web-app-builder",
  "bundle_version": "1.0",              // 省略则用最新启用版本
  "input": { "requirements": "做一个待办事项 Web 应用" }
}

// Response 201
{
  "code": 0,
  "message": "ok",
  "data": {
    "run_id": "run-9e67931d5c38024e",
    "status": "running",
    "created_at": "2026-08-20T13:10:00Z"
  },
  "request_id": "req-a1b2c3d4"
}

// Response 422（图校验失败）
{
  "code": 40003,
  "message": "Bundle 编排图存在无法触发的节点",
  "data": null,
  "request_id": "req-a1b2c3d4",
  "details": [
    { "field": "orchestration.edges[3]", "reason": "节点 fullstack_engineer 的自环边被计入 join 前置条件，将导致该节点永远无法首次触发" }
  ]
}
```

---

## 七、关键技术方案

### Bundle 编排引擎：ADK Go 2.0 集成方案

**选型已定**。调研过手写引擎、Goa-AI、Google ADK Go 2.0 三个方向，最终采用 **ADK Go 2.0**：

| 需求 | POC 阶段手写方案 | ADK Go 2.0（采用） |
|---|---|---|
| 图执行（并行/汇聚/条件路由） | 手写，真实踩过自环边死锁的 bug | 原生图执行引擎，Agent/Tool/Function 都是图节点 |
| Human-in-the-loop | 内存 channel，重启即丢失 | 内置原语，支持"请求确认"流程 |
| 崩溃恢复 | 无 | 进程重启后状态可恢复 |
| 可观测性 | 自己定义 Event 类型 | 原生 OpenTelemetry |
| 生态 / 生产信号 | 无 | 20,000+ star，多家企业生产在用 |

**分层边界**——这是集成的关键，两层各管各的，不要混：

```
┌──────────────────────────────────────────────────────────┐
│  平台层（自研，POC 已验证的产品抽象，ADK 不提供）            │
│  · Agent DSL / Bundle DSL（JSON Schema 校验）              │
│  · 资源中心的能力白名单授权                                 │
│  · 统一响应信封、错误码、事件流 NDJSON 协议                  │
│  · 团队/权限/审批人角色校验                                 │
└───────────────────────┬──────────────────────────────────┘
                        │ DSL → ADK 图定义的编译层（本期核心工作量）
┌───────────────────────▼──────────────────────────────────┐
│  ADK Go 2.0（引擎，不改其源码）                             │
│  · 图执行调度、并行/条件分支                                │
│  · human-in-the-loop 暂停与恢复                            │
│  · session/state 持久化、崩溃恢复                          │
│  · 模型调用、工具执行、OTel 埋点                            │
└──────────────────────────────────────────────────────────┘
```

**DSL → ADK 的映射约定**（`internal/orchestrator/compiler` 负责）：

| 平台 DSL 概念 | 映射到 ADK |
|---|---|
| `agent` 定义（persona/model/tools） | 一个 ADK Agent 节点 |
| `bundle.orchestration.edges` | ADK 图的节点与边；`mode: parallel` 映射并行分支 |
| `edge.condition` | ADK 的条件边（表达式求值改用 `expr-lang/expr`，不再用 POC 的简易求值器） |
| `human_gates[].after` | ADK 的 human-in-the-loop 确认节点，挂在对应节点之后 |
| `shared_state` | ADK 的 session state，平台侧只做结构化字段约定和 Schema 校验 |
| `capabilities.tools/skills` | 编译期解析成 ADK Tool 实例；资源中心校验在编译前完成，未授权的资源不进入图 |
| `constraints`（超时/最大工具调用数） | ADK 节点级配置 + 平台层兜底熔断 |

**平台事件流与 ADK 事件的对接**：ADK 产出自己的执行事件，平台侧订阅后翻译成 POC 已验证的事件模型（`node.started`/`node.thinking`/`node.tool_call.*`/`human_gate.*`），写入 `bundle_run_events` 表，再经 `/stream` 接口推给前端。**保留这层翻译而不是直接透传 ADK 事件**，理由是前端契约不应该跟着上游框架的版本演进走——ADK 从 1.x 到 2.0 有过破坏性变更，这层隔离是有价值的。

**已知风险**：ADK Go 2.0 于 2026 年 6 月 GA，距今约两个月，API 仍在快速演进。缓解措施：（1）所有 ADK 调用收敛在 `internal/orchestrator/adk` 一个包内，不散落到业务代码；（2）保留 POC 验证过的 `AgentRunner` 接口作为适配层边界，必要时可替换实现；（3）锁定 ADK 次版本号，升级走独立评估。

### 黑盒资源的运行时链路

这是"黑盒发布 + 快照隔离"两个产品决策在运行时的具体落地，也是最容易出安全漏洞的地方。完整链路：

```
订阅者点击运行
      │
      ▼
① Bundle Orchestrator 收到 POST /runs，鉴权通过（订阅者身份）
      │
      ▼
② 查 subscriptions：该用户是否订阅了这个 listing？
   没订阅 → 403（不能靠"知道 ID 就能跑"）
      │
      ▼
③ 经 Marketplace Service 解析 listing_id → 作者的具体版本定义
   注意：查的是订阅时绑定的那一行，不是"最新版本"（快照隔离）
      │
      ▼
④ 依赖完整性复核：定义里引用的资源当前是否仍可用
   （发布时已校验过，但作者可能事后禁用了某个资源）
   不可用 → 422，错误信息脱敏，只说"该资源当前不可用，请联系作者"
      │
      ▼
⑤ 编译成 ADK 图并执行，运行标记 via_listing_id = 该 listing
      │
      ▼
⑥ 事件落库时按类型标记 is_internal
   （node.thinking / 含 persona 的 payload → is_internal = true）
      │
      ▼
⑦ /stream 接口推送时，若 run.via_listing_id 非空且请求者非作者，
   过滤掉 is_internal = true 的事件；执行图接口返回占位节点结构
      │
      ▼
⑧ Token 消耗计入订阅者账户（用订阅者配置的 Provider 凭证）
```

**几个容易踩的点**：

- **第 ② 步不能省**。如果只校验"listing 是否存在"，那么任何人拿到 listing_id 就能免订阅运行别人的资源
- **第 ④ 步的错误信息必须脱敏**。直接把"MCP Server internal-search 连接失败"回传给订阅者，等于泄露了作者的内部资源名
- **第 ⑦ 步的判断条件是"请求者非作者"而非"存在 via_listing_id"**。作者自己排查问题时需要看到完整事件流，不能一刀切过滤
- **`shared_state` 也可能泄露内部信息**。如果黑盒 Bundle 的中间节点往 shared_state 写了内部提示词片段，订阅者在 Chat 页的"共享状态"面板就能看到。V1 的处理：黑盒运行时，共享状态面板只展示 `display_meta.io_description` 中声明的输出字段，其余字段隐藏

### 事件流方案与多实例挑战

POC 阶段验证过的方案：chunked HTTP + NDJSON，服务端一个 goroutine 内部按 300ms 间隔查 SQLite 的增量事件、`Flush()` 给客户端，单实例下完整跑通、实测确认延迟低、连接数可控。

**这个方案在多实例部署下有一个必须解决的新问题**：如果 Bundle Orchestrator 水平扩展成多个实例，某次运行的引擎 goroutine 跑在实例 A 上，但用户的浏览器连接可能被负载均衡器分到实例 B 的 `/stream` 接口——实例 B 本地没有这次运行的实时事件。两个方案：

1. **会话粘性（sticky session）**：负载均衡器保证同一个 `run_id` 的请求始终路由到同一实例。实现简单，但实例故障时这次运行的连接会中断，需要客户端重连到新实例、新实例从数据库补历史事件（这部分逻辑复用已验证的"先写历史再进入循环"设计，天然支持）。
2. **共享事件总线**：所有实例往一个外部总线（PostgreSQL `LISTEN/NOTIFY` 或 Redis Streams）发布事件，每个实例的 `/stream` handler 订阅总线而不是只查本地看到的数据库行。更健壮，但多引入一个组件。

【假设】V1 阶段用方案一（会话粘性）起步，实例数不多、可用性要求还没到"单实例故障不能有感知"的程度；实例数上来后再评估切换到方案二。

### Human Gate 持久化

POC 阶段的 `ChannelGateProvider`（内存 channel）明确标注过"服务重启会丢失所有待处理的 gate"这个局限。生产方案直接使用 **ADK Go 2.0 内置的 human-in-the-loop 能力**：暂停状态随 session state 一起持久化，进程重启后可恢复，不需要平台层自己维护等待队列。

平台层仍需承担两件 ADK 不管的事：

1. **审批人权限校验**：ADK 只负责"暂停等待外部输入"，"谁有资格提供这个输入"是平台的安全边界，在 `POST /runs/{id}/gate` 里做二次角色校验
2. **超时策略**：`human_gates[].timeout_seconds` 与 `on_timeout`（`auto_approve`/`auto_reject`/`abort`）由平台侧定时任务扫描待处理 gate 实现，超时后主动向 ADK 注入对应结果

### 高可用设计

```
服务层：
- 所有服务无状态设计（编排引擎的运行状态落库，不放在内存里，这样才能支持上面的会话粘性 + 多实例）
- 熔断降级：模型调用失败率超阈值触发熔断，Bundle 运行标记为"模型不可用"而不是无限重试
- 超时控制：Agent 执行超时按 DSL 里的 constraints.timeout_seconds 强制生效

数据层：
- PostgreSQL 主从，主库故障自动切换
- 每日全量备份 + WAL 归档

基础设施：
- 多可用区部署
- 健康检查：/health 接口
```

---

## 八、非功能需求

| 需求 | 指标 | 方案 |
|---|---|---|
| 可用性 | 99.5%（V1 阶段，非全对外 SaaS 标准） | 多实例 + 数据库主从 |
| 事件流延迟 | 服务端检测间隔 300ms（对应用户感知延迟） | chunked HTTP 方案，已验证 |
| 数据安全 | 模型 API Key 等凭证加密存储 | 数据库字段级加密（AES-256），不落明文日志 |
| 权限粒度 | 查看/编辑/运行/审批四级 | IAM Service 统一鉴权，Bundle Orchestrator 在执行 human gate 前二次校验审批人角色 |
| 可观测性 | Bundle 运行全链路可追溯 | 复用已验证的事件模型；引入 ADK Go 2.0 后叠加 OpenTelemetry |
| 审计 | 谁在什么时候批准/驳回了哪个 human gate，不可篡改 | audit_logs 表只追加（append-only），human gate 的审批记录额外落一份到审计表 |
| 接口响应时间 | 常规 CRUD P99 < 300ms（不含模型调用耗时） | 索引覆盖查询热点；模型调用一律异步化，不阻塞 HTTP 请求 |
| 接口限流 | 单用户 600 次/分钟；`POST /runs` 单团队 20 次/分钟 | 网关层令牌桶，超限返回 429 + `10002`，响应头带 `Retry-After` |
| 前后端契约一致性 | 100% 接口有 OpenAPI 定义 | `api/openapi.yaml` 作为唯一事实来源，CI 中校验实现与规范一致，不一致则构建失败 |

---

## 九、部署架构

```
生产环境：
  Kubernetes 集群
  ├── web-console：Deployment，静态资源走 CDN
  ├── api-gateway：Deployment，副本数 ≥ 2
  ├── bundle-orchestrator：Deployment，副本数 ≥ 2，会话粘性路由
  ├── agent-service / resource-registry / model-gateway / operation-service / iam-service：
  │     各自 Deployment，副本数 ≥ 2
  ├── 配置管理：ConfigMap + Secret（模型 Provider 凭证走 Secret）
  └── 入口：Ingress → api-gateway

CI/CD 流程：
  代码提交 → CI（单测 + DSL Schema 校验测试）→ 构建镜像
  → 灰度发布（先切一部分 Bundle Orchestrator 副本验证图执行逻辑无回归）
  → 全量发布
```

---

## 十、风险与演进路径

**当前方案的局限性**：

- **风险 1：ADK Go 2.0 选型未经 spike 验证** → 缓解措施：M1 阶段并行做 spike，架构里编排引擎相关的接口（`AgentRunner` 抽象）保持可替换，不管 spike 结果如何都不影响其他模块
- **风险 2：多实例下的事件流方案（会话粘性）在实例故障时有短暂中断** → 缓解措施：客户端重连 + 补历史事件的逻辑已在 POC 阶段验证过机制可行，M1 阶段需要补充自动重连的前端逻辑
- **风险 3：资源中心的四类资源（tool/skill/mcp/知识库）目前用一张表统一抽象，实际接入时可能发现类型差异大到不适合共享表结构** → 缓解措施：`config` 字段用 JSONB 承载差异化配置，真出现无法共享的情况再拆分表，不提前过度设计
- **风险 4：用户级权限模型在真正多租户场景下不够用** → **已决**：V1 明确不做多租户（PRD 非目标第 5 条），资源归属到 `owner_user_id`，权限规则为「自己的 + 订阅的」。真要做多租户时的演进路径是加 `tenant_id` 并启用 PostgreSQL 行级安全策略（RLS），届时 `owner_user_id` 的语义不变，是可加法演进而非重构

**演进路径**：

```
阶段一（M0-M1，当前）：单体化部署起步，SQLite → PostgreSQL 单实例，
                      会话粘性路由，核心验证"平台能不能把 POC 阶段
                      验证的机制产品化"

阶段二（M2，DAU 增长后）：Bundle Orchestrator 多实例 + 共享事件总线，
                        模型中心/运营中心补齐，PostgreSQL 主从

阶段三（业务明确要多租户/对外开放后）：行级数据隔离，A2A 协议对接
                                    （评估是否需要接入外部 Agent），
                                    资源市场的第三方生态
```
