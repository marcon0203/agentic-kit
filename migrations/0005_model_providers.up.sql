CREATE TABLE model_providers (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    provider        VARCHAR(16)   NOT NULL,
    credentials     BYTEA         NOT NULL,  -- AES-256 加密
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);
