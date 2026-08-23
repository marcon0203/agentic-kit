-- spec-18: operations center — admin flag for moderation, and a reports
-- queue for marketplace listings. audit_logs already exists (0010) and is
-- already append-only (trigger-enforced); this just adds a way to browse it.

ALTER TABLE users
    ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE reports (
    id               BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    listing_id       BIGINT        NOT NULL REFERENCES marketplace_listings(id),
    reporter_user_id BIGINT        NOT NULL REFERENCES users(id),
    reason           TEXT          NOT NULL,
    status           VARCHAR(16)   NOT NULL DEFAULT 'pending',
        -- pending / resolved
    resolution       VARCHAR(16),
        -- dismissed / taken_down — set when status becomes resolved
    resolved_by      BIGINT        REFERENCES users(id),
    resolved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_reports_status ON reports (status, created_at DESC);
