-- Backs the ApiKeyAuth security scheme (api/openapi.yaml, "Authorization:
-- ApiKey <key>") for server-to-server / CI callers, running in parallel
-- with JWT per spec-04's acceptance checklist. Only the SHA-256 hash of the
-- key is stored — the raw key is shown once at creation time and is not
-- recoverable afterward, the same model GitHub/Stripe use for PATs.
CREATE TABLE api_keys (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    name            VARCHAR(64)   NOT NULL,
    key_hash        VARCHAR(64)   NOT NULL,  -- sha256 hex digest of the raw key
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (key_hash)
);

CREATE INDEX idx_api_keys_owner_user_id ON api_keys (owner_user_id);
