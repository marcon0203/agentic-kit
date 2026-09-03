-- MCP 源：管理员登记一个公开的 MCP 注册中心（如官方
-- https://registry.modelcontextprotocol.io），同步后其公开 Server 进入
-- MCP 管理 → 市场视图。
--
-- 表形状与 skill_sources / market_skills 一致（同一套"登记—同步—审核—
-- 安装"流程），差别只在条目字段：MCP 条目的关键信息是"怎么连上它"
-- （remote_url / remote_type）而不是"装什么包"。
--
-- 审核列从建表起就在，不像 skill 那样拆两个迁移：同步进来的公开条目默认
-- pending，没有一个"先全量放行再补审核"的中间状态需要兼容。
CREATE TABLE mcp_sources (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    name            TEXT        NOT NULL,
    base_url        TEXT        NOT NULL,            -- 形如 https://registry.modelcontextprotocol.io，不带尾斜杠
    status          SMALLINT    NOT NULL DEFAULT 1,  -- 1 启用 2 停用，同 resources 约定
    last_synced_at  TIMESTAMPTZ,
    last_sync_error TEXT,                            -- 非空 = 上次同步失败，页面直接展示
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (base_url)
);

CREATE TABLE market_mcp_servers (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id       BIGINT      NOT NULL REFERENCES mcp_sources(id) ON DELETE CASCADE,
    -- slug 是上游的限定名（如 io.github.owner/airtable-mcp-server）。它带
    -- 点和斜杠，做不了 URL 路径参数，所以对外一律用本行的 id 寻址。
    slug            TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    summary         TEXT,
    version         TEXT,
    license         TEXT,
    repository_url  TEXT,
    -- remote_url / remote_type 是"能不能一键接入"的判据：只有给了远端地址
    -- 的条目才装得上，只能本地起进程的（packages）在页面上标为不可安装。
    remote_url      TEXT,
    remote_type     TEXT,                            -- streamable-http | sse
    topics          TEXT[]      NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMPTZ,                     -- 上游的更新时间
    raw             JSONB       NOT NULL DEFAULT '{}',
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    review_status   VARCHAR(16) NOT NULL DEFAULT 'pending', -- pending | approved | rejected
    review_note     TEXT,
    reviewed_at     TIMESTAMPTZ,
    reviewed_by     BIGINT REFERENCES users(id),

    UNIQUE (source_id, slug)
);

CREATE INDEX market_mcp_servers_source_idx ON market_mcp_servers (source_id);
CREATE INDEX market_mcp_servers_review_idx ON market_mcp_servers (review_status);
