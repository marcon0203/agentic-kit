-- Splits the unified `resources` table (migration 0002) into four
-- independent tables, one per resource type. See spec-05-resource-center.md
-- "分表设计" for why: mcp_servers needs a `health` column the other three
-- have no use for, and that difference was clear from the start rather than
-- discovered later, so there's no reason to wait on splitting.
DROP TABLE resources;

CREATE TABLE tools (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, ref, version)
);

CREATE TABLE skills (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, ref, version)
);

CREATE TABLE mcp_servers (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    health          VARCHAR(16)   NOT NULL DEFAULT 'unknown',
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, ref, version)
);

CREATE TABLE knowledge_bases (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    ref             VARCHAR(64)   NOT NULL,
    version         VARCHAR(16)   NOT NULL DEFAULT '1.0',
    config          JSONB         NOT NULL,
    display_meta    JSONB         NOT NULL DEFAULT '{}',
    status          SMALLINT      NOT NULL DEFAULT 1,
    immutable       BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, ref, version)
);

-- Re-attach the same immutable-row backstop (functions defined in
-- migration 0006) to all four new tables.
CREATE TRIGGER tools_reject_immutable_update
  BEFORE UPDATE ON tools
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER tools_reject_immutable_delete
  BEFORE DELETE ON tools
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

CREATE TRIGGER skills_reject_immutable_update
  BEFORE UPDATE ON skills
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER skills_reject_immutable_delete
  BEFORE DELETE ON skills
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

CREATE TRIGGER mcp_servers_reject_immutable_update
  BEFORE UPDATE ON mcp_servers
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER mcp_servers_reject_immutable_delete
  BEFORE DELETE ON mcp_servers
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

CREATE TRIGGER knowledge_bases_reject_immutable_update
  BEFORE UPDATE ON knowledge_bases
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER knowledge_bases_reject_immutable_delete
  BEFORE DELETE ON knowledge_bases
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();
