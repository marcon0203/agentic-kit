DROP TABLE knowledge_bases;
DROP TABLE mcp_servers;
DROP TABLE skills;
DROP TABLE tools;

CREATE TABLE resources (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    type            VARCHAR(16)   NOT NULL,
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, type, ref, version)
);

CREATE TRIGGER resources_reject_immutable_update
  BEFORE UPDATE ON resources
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER resources_reject_immutable_delete
  BEFORE DELETE ON resources
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();
