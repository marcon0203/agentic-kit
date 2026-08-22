# spec-02-database 数据库 schema 与数据访问层

> 对应任务 `task_implement.json#2` | 依赖：#1 app_init
> 关联：docs/架构设计文档_AI-Agent平台_V1.md、docs/PRD_AI-Agent平台_AgentBundle编排与运行_V1.md、api/openapi.yaml

## 目标

建立全部数据表、迁移脚本和类型安全的数据访问层。

## 迁移文件顺序

```
0001_users.up.sql
0002_resources_agents_bundles.up.sql
0003_marketplace.up.sql
0004_runs_events.up.sql
0005_model_providers.up.sql
0006_immutable_trigger.up.sql
```

表结构见架构文档第五章，此处不重复。

## 关键实现

### immutable 触发器

已被订阅的资源版本不可修改，**数据库层兜底**而不只靠应用层：

```sql
CREATE OR REPLACE FUNCTION reject_immutable_update() RETURNS trigger AS $$
BEGIN
  IF OLD.immutable THEN
    RAISE EXCEPTION '该版本已被订阅，不可修改（快照隔离）';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- 对 agents / bundles / resources 三张表分别建 BEFORE UPDATE OR DELETE 触发器
```

### sqlc 查询分组

owner 视角与 subscriber 视角**物理分离**（见 spec-08）：`GetXxxForOwner` 返回全字段，`GetXxxDisplayForSubscriber` 的 SELECT 列表里根本没有 `definition`。

## 验收清单

- [ ] `make migrate-up` / `migrate-down` 均可反复执行且幂等
- [ ] sqlc 生成代码编译通过，无手写 SQL 散落在业务层
- [ ] testcontainers 起真实 PostgreSQL 跑集成测试（不用 sqlmock，避免掩盖真实 SQL 行为）
- [ ] 尝试 UPDATE 已置 `immutable` 的行，数据库抛异常
- [ ] 所有外键约束与索引按架构文档创建完毕
