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

CREATE INDEX idx_bundle_status ON bundle_runs (bundle_id, status);
CREATE INDEX idx_triggered_by ON bundle_runs (triggered_by, created_at DESC);

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

CREATE INDEX idx_run_id ON bundle_run_events (run_id, id);
