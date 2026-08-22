# spec-04-auth 用户体系与认证

> 对应任务 `task_implement.json#4` | 依赖：#2 database、#3 api_foundation
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

用户注册登录、JWT 鉴权，以及贯穿全站的资源归属校验。

## 归属模型

资源归属到**用户**（`owner_user_id`），不是团队。每个用户拥有独立资源空间，默认只能访问自己的资源 + 自己订阅的资源。

## 关键实现

- 密码哈希用 **argon2id**（不是 bcrypt/MD5）
- JWT access token 2h，refresh token 7d
- 鉴权中间件把当前用户注入 `context`，后续 handler 一律从 context 取，不从参数传
- 提供 `RequireOwner(resourceOwnerID)` helper，统一归属校验逻辑

## 验收清单

- [ ] 注册重复邮箱返回 409
- [ ] 密码强度校验（≥8 位），明文永不落库、永不进日志
- [ ] 登录成功返回 access + refresh token 与用户信息
- [ ] 过期 token 返回 401 + 20002；无效 token 返回 401 + 20001
- [ ] 访问他人资源返回 403 + 20003
- [ ] API Key 鉴权路径与 JWT 并行可用
