CREATE TABLE marketplace_listings (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    author_user_id  BIGINT        NOT NULL REFERENCES users(id),
    resource_type   VARCHAR(16)   NOT NULL,  -- agent/bundle/skill/mcp
    resource_id     BIGINT        NOT NULL,  -- 指向上述表的具体版本行
    listing_ref     VARCHAR(64)   NOT NULL,  -- 广场内的稳定标识（跨版本不变）
    version         VARCHAR(16)   NOT NULL,
    visibility      VARCHAR(16)   NOT NULL DEFAULT 'blackbox',
        -- V1 仅 blackbox；预留 public 供后续"公开可复制"模式
    changelog       TEXT,         -- 作者手写的更新说明（不是 diff，diff 会泄露黑盒内容）
    distribution    SMALLINT      NOT NULL DEFAULT 1,
        -- 1 分发中 / 2 已停止分发（不影响存量订阅）/ 3 已下架（举报处理）
    subscriber_count INTEGER      NOT NULL DEFAULT 0,  -- 冗余计数，避免列表页 count(*)
    run_count       BIGINT        NOT NULL DEFAULT 0,
    published_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (listing_ref, version)
);

CREATE INDEX idx_type_distribution ON marketplace_listings (resource_type, distribution, published_at DESC);

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
