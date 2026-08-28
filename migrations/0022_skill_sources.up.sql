-- Skill 源（系统配置 → Skill 源）：管理员登记一个公开的 Skill 市场地址
-- （如 https://clawhub.ai/），同步后其公开 Skill 进入 Skill 管理的市场视图。
-- 同步结果是本地缓存（market_skills），详情/版本历史按需回源拉取，避免
-- 每次同步对上游做 N+1 请求。
CREATE TABLE skill_sources (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    name            TEXT        NOT NULL,
    base_url        TEXT        NOT NULL,            -- 形如 https://clawhub.ai，不带尾斜杠
    status          SMALLINT    NOT NULL DEFAULT 1,  -- 1 启用 2 停用，同 resources 约定
    last_synced_at  TIMESTAMPTZ,
    last_sync_error TEXT,                            -- 非空 = 上次同步失败，页面直接展示
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (base_url)
);

-- 从某个源同步下来的公开 Skill 快照。raw 保留上游列表接口的原始条目，
-- 字段解析宽容（slug/displayName/summary/stats 各写一份常用列），这样
-- 上游加字段时详情页不需要重新迁移就能展示。
CREATE TABLE market_skills (
    id          BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id   BIGINT      NOT NULL REFERENCES skill_sources(id) ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    summary     TEXT,
    version     TEXT,
    license     TEXT,
    changelog   TEXT,
    topics      TEXT[]      NOT NULL DEFAULT '{}',
    stars       BIGINT      NOT NULL DEFAULT 0,
    downloads   BIGINT      NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ,                         -- 上游的 updatedAt，毫秒纪元
    raw         JSONB       NOT NULL DEFAULT '{}',
    synced_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, slug)
);

CREATE INDEX market_skills_source_idx ON market_skills (source_id);
