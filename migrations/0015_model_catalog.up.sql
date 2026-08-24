CREATE TABLE catalog_providers (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    provider_key    VARCHAR(32)   NOT NULL UNIQUE,
    display_name    VARCHAR(64)   NOT NULL,
    icon            TEXT,
    base_url        VARCHAR(512),
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE catalog_models (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    provider_id     BIGINT        NOT NULL REFERENCES catalog_providers(id) ON DELETE CASCADE,
    model           VARCHAR(128)  NOT NULL,
    display_name    VARCHAR(128)  NOT NULL,
    description     TEXT          NOT NULL DEFAULT '',
    modality        VARCHAR(16)   NOT NULL,
    featured        BOOLEAN       NOT NULL DEFAULT false,
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (provider_id, model)
);

CREATE INDEX idx_catalog_models_provider_id ON catalog_models(provider_id);
