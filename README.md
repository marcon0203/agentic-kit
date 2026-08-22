# AI Agent 平台

Agent/Bundle 编排与运行平台。设计与工程规格见 [`PLATFORM_README.md`](./PLATFORM_README.md)、
[`docs/`](./docs)、[`spec/`](./spec)、[`task_implement.json`](./task_implement.json)。

当前进度：`task_implement.json#1`（项目脚手架初始化）已完成，后续任务按 `depends_on`
顺序推进。

## 目录结构

```
.
├── api/openapi.yaml          # API 契约，唯一事实来源
├── cmd/server/main.go        # 后端入口
├── internal/
│   ├── api/                  # HTTP handler 与中间件
│   ├── store/                # sqlc 生成代码与查询
│   ├── orchestrator/         # 编排引擎与 ADK 集成
│   ├── marketplace/          # 广场与订阅
│   └── config/                # viper 配置
├── migrations/                # golang-migrate SQL 文件
├── schemas/                   # agent.schema.json / bundle.schema.json
├── web/                        # React 前端
├── docker-compose.yml
├── Makefile
└── .github/workflows/ci.yml
```

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
