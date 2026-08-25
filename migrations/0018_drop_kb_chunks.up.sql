-- 知识库 retrieval moved off pgvector onto Milvus (vector search) +
-- Elasticsearch (keyword search / 多路召回) — see
-- internal/adapter/milvus and internal/adapter/elasticsearch. This table
-- and the pgvector extension it required are no longer used by anything.
DROP TABLE kb_chunks;
DROP EXTENSION IF EXISTS vector;
