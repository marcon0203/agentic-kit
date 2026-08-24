// Package knowledgebase is real document retrieval for the 知识库
// resource kind (spec-05): chunk, embed and store a document's text, then
// answer a query with its nearest chunks by cosine similarity over
// pgvector. Before this package existed, "knowledge_base" was just a
// labeled config record with no actual index — this is what makes it real.
package knowledgebase

import "time"

// EmbeddingDimension is the fixed vector width every knowledge base's
// registered embedding model must produce — migrations/0013_kb_chunks.up.sql
// pins the storage column to vector(1536), matching the width OpenAI's
// ada-002/text-embedding-3-small (and most OpenAI-compatible embedding
// models) produce.
const EmbeddingDimension = 1536

// Chunk is one stored unit of a document: an embeddable slice of its text.
type Chunk struct {
	ID         int64
	SourceRef  string
	ChunkIndex int
	Content    string
}

// SearchResult is a Chunk ranked by relevance to a query. Score is
// 1 - cosine distance, so higher is more similar (0..1 for normalized
// embeddings) — the sign flip is so callers never have to remember which
// direction "better" points.
type SearchResult struct {
	Chunk
	Score float64
}

// SourceSummary is what the resource's document list shows: one row per
// ingested source, not per chunk.
type SourceSummary struct {
	SourceRef  string
	ChunkCount int
	IngestedAt time.Time
}
