# AI Agent 平台

Agent/Bundle 编排与运行平台。设计与工程规格见 [`PLATFORM_README.md`](./PLATFORM_README.md)、
[`docs/`](./docs)、[`spec/`](./spec)、[`task_implement.json`](./task_implement.json)。

当前进度：`task_implement.json` 的 19 个任务已全部完成，代码按 DDD 分层重构完毕
（见下方"分层约定"）。

## 目录结构

```
.
├── api/openapi.yaml          # API 契约，唯一事实来源
├── cmd/server/main.go        # 后端入口，负责把 domain 与 adapter 装配起来
├── internal/
│   ├── domain/               # 业务规则：每个限界上下文一个包，不依赖任何基础设施
│   │   ├── agent/            #   应用中心 · Agent
│   │   ├── bundle/           #   应用中心 · Bundle（两级校验：阻断 vs 警告）
│   │   ├── resource/         #   资源中心（凭证从响应中"消失"的规则在这里）
│   │   ├── run/              #   编排运行时（黑盒可见性、审批 gate、熔断）
│   │   ├── marketplace/      #   广场与订阅（快照隔离、黑盒分发）
│   │   ├── modelcenter/      #   模型中心（Provider 连通性校验 + 用量）
│   │   ├── operation/        #   运营中心（举报队列、下架、审计日志）
│   │   ├── iam/              #   注册登录与令牌
│   │   └── codes.go          #   五位业务错误码表（业务语言，不是传输细节）
│   ├── adapter/              # 端口的实现：domain 声明接口，这里对接真实设施
│   │   ├── postgres/         #   sqlc/pgx，把 23505、ErrNoRows 翻译成领域哨兵错误
│   │   ├── orchestrator/     #   ADK 编译与执行、gate channel 注册表
│   │   ├── crypto/ password/ #   AES-256-GCM / argon2id
│   │   ├── modelgateway/     #   Provider 连通性探测
│   │   └── mcp/ schema/      #   MCP 健康探测 / JSON Schema 校验
│   ├── api/                  # HTTP 传输层：只做 JSON、状态码、路由
│   ├── store/                # sqlc 生成代码与查询
│   ├── orchestrator/adk/     # ADK 编译器本体（被 adapter/orchestrator 使用）
│   └── config/               # viper 配置
├── migrations/               # golang-migrate SQL 文件
├── schemas/                  # agent.schema.json / bundle.schema.json
├── web/                      # React 前端
├── docker-compose.yml
├── Makefile
└── .github/workflows/ci.yml
```

### 分层约定

依赖只向内指：`api` → `domain` ← `adapter`。领域包不 import `net/http`、`chi`、`pgx`、
`internal/store`、`internal/api` 或 `internal/adapter` —— 这条规则由
`internal/domain/layering_test.go` 在 CI 中强制执行，而不只是写在文档里。

因此业务规则的测试全部跑在内存假实现上（无需 Postgres），而
`internal/api` 的测试只覆盖传输层本身：DTO 形状、状态码、NDJSON 分帧。

## 本地开发

```bash
cp .env.example .env   # 按需修改 JWT_SECRET / CREDENTIAL_AES_KEY
make dev                # 一条命令拉起 PostgreSQL + 后端 + 前端
```

其他常用命令：`make build` / `make test` / `make lint` / `make migrate-up` / `make sqlc-gen`。

## 技术栈

- 后端：Go 1.24.7 + chi v5.1.0 + sqlc v1.27.0 / pgx v5.7.1 + PostgreSQL 16.4
- 前端：React 19.0.0 + TS 5.7.2 + Vite 6.2.0 + TanStack Query 5.62.7 + Zustand 5.0.2 +
  Tailwind 4.0.17 + shadcn/ui（源码在 `web/src/components/ui/`）
- 编排引擎：Google ADK Go 2.0（`spec-10-adk-integration` 起接入）
