-- Backs the Idempotency-Key middleware (架构设计文档 6.6): a repeated POST
-- with the same key inside the TTL window returns the first response
-- instead of creating a duplicate — a Bundle run in particular can burn a
-- lot of tokens, so a network-retry duplicate is a real cost, not just a
-- data-integrity nuisance.
CREATE TABLE idempotency_keys (
    key             VARCHAR(128)  PRIMARY KEY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    status_code     INTEGER       NOT NULL,
    response_body   BYTEA         NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ   NOT NULL
);

CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys (expires_at);
