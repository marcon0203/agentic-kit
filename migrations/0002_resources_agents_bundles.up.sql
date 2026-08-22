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
