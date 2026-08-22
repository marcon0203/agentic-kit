# spec-01-app-init 项目脚手架初始化

> 对应任务 `task_implement.json#1` | 依赖：无
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

搭起前后端工程骨架、本地开发环境和 CI，让后续任务有地方落代码。

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
│   └── config/               # viper 配置
├── migrations/               # golang-migrate SQL 文件
├── schemas/                  # agent.schema.json / bundle.schema.json
├── web/                      # React 前端
├── docker-compose.yml
├── Makefile
└── .github/workflows/ci.yml
```

## 技术栈与版本锁定（详见架构文档第三章）

**所有依赖锁定精确补丁版本，禁止 `^` / `~` / `latest`**：

- Go：`go.mod` 精确版本 + 提交 `go.sum`
- 前端：提交 `package-lock.json`，CI 一律 `npm ci`（不是 `npm install`）
- 镜像：带 digest 的精确标签，如 `postgres:16.4-alpine@sha256:...`
- GitHub Actions：用 commit SHA 固定，不用 `@v4` 浮动标签

**shadcn/ui 走 CLI 方式**，不装运行时依赖包：

```bash
npx shadcn@2.1.8 init
npx shadcn@2.1.8 add button input card dialog tabs table badge select checkbox textarea toast skeleton
```

组件源码落在 `src/components/ui/` 并**纳入版本控制**，之后直接改这些文件；平台自定义组件放 `src/components/ds/`，两个目录分开。

## Makefile 目标

`build` / `test` / `lint` / `migrate-up` / `migrate-down` / `sqlc-gen` / `openapi-lint` / `dev`

## 验收清单

- [ ] `make dev` 一条命令拉起 PostgreSQL + 后端 + 前端
- [ ] `make test` 通过（含一个占位集成测试，验证 testcontainers 可用）
- [ ] `make lint` 通过（golangci-lint + tsc --noEmit + redocly lint）
- [ ] CI 在 PR 上自动跑通全部检查
- [ ] `.env.example` 覆盖全部必需配置项，缺项时启动报错清晰
- [ ] `go.mod` / `package.json` 中**没有任何**浮动版本符号（`^` `~` `latest` `x.y+`）
- [ ] `go.sum` 与 `package-lock.json` 已提交
- [ ] CI 使用 `npm ci` 而非 `npm install`
- [ ] Docker 镜像标签带 digest
- [ ] shadcn 组件已通过 CLI 安装到 `src/components/ui/` 并纳入版本控制
