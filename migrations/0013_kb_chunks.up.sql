-- Real vector retrieval for 知识库 (spec-05's knowledge_base kind existed as
-- a labeled config record with no actual document index). kb_chunks is
-- deliberately its own table, independent of knowledge_bases' immutable-row
-- tracking: documents get ingested/removed over the KB's lifetime regardless
-- of which Agent version pinned the KB resource ref itself.
CREATE EXTENSION IF NOT EXISTS vector;

-- 1536 matches OpenAI-compatible ada-002/text-embedding-3-small and is the
-- most common embedding width across providers; a knowledge base's
-- registered embedding model must produce vectors of this width (checked at
-- ingest time, not by the schema).
CREATE TABLE kb_chunks (
    id                BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    knowledge_base_id BIGINT        NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    owner_user_id     BIGINT        NOT NULL REFERENCES users(id),
    source_ref        VARCHAR(255)  NOT NULL,
    chunk_index       INT           NOT NULL,
    content           TEXT          NOT NULL,
    embedding         vector(1536)  NOT NULL,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX kb_chunks_kb_id_idx ON kb_chunks (knowledge_base_id, owner_user_id);
CREATE INDEX kb_chunks_embedding_idx ON kb_chunks USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
