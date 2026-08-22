CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    email           VARCHAR(255)  NOT NULL,
    password_hash   VARCHAR(255)  NOT NULL,  -- argon2id
    display_name    VARCHAR(64)   NOT NULL,
    status          SMALLINT      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (email)
);
