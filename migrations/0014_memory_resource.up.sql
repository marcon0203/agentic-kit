-- 记忆库 resource kind, mirroring the tools/skills/mcp_servers/knowledge_bases
-- split-table pattern (spec-05). A memory resource is the "shell" (config,
-- status, ownership) registered in 资源中心; memory_entries is the real
-- store behind it — what internal/adapter/postgres.MemoryService writes
-- to/reads from as ADK's own memory.Service interface (AddSessionToMemory /
-- SearchMemory).
CREATE TABLE memories (
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

CREATE TRIGGER memories_reject_immutable_update
  BEFORE UPDATE ON memories
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER memories_reject_immutable_delete
  BEFORE DELETE ON memories
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_delete();

-- One row per (session, event) ADK asks to remember. Scoped by memory_id
-- (which registered memory resource) *and* app_name/agent_user_id (the
-- ADK-level identity a SearchRequest carries) — the first is this
-- platform's resource-ownership boundary, the second is ADK's own.
CREATE TABLE memory_entries (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    memory_id       BIGINT        NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    owner_user_id   BIGINT        NOT NULL REFERENCES users(id),
    app_name        VARCHAR(128)  NOT NULL,
    agent_user_id   VARCHAR(64)   NOT NULL,
    session_id      VARCHAR(64)   NOT NULL,
    author          VARCHAR(64)   NOT NULL DEFAULT '',
    content         TEXT          NOT NULL,
    content_tsv     TSVECTOR      GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX memory_entries_scope_idx ON memory_entries (memory_id, owner_user_id, app_name, agent_user_id);
CREATE INDEX memory_entries_content_tsv_idx ON memory_entries USING GIN (content_tsv);
