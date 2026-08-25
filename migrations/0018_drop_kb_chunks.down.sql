CREATE EXTENSION IF NOT EXISTS vector;

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
