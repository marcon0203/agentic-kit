// Package knowledgebase is real document retrieval for the 知识库
// resource kind (spec-05): chunk, embed and store a document's text, then
// answer a query with 多路召回 (multi-route recall) — nearest chunks by
// cosine similarity from Milvus, fused via Reciprocal Rank Fusion with
// BM25 keyword matches from Elasticsearch. Before this package existed,
// "knowledge_base" was just a labeled config record with no actual index —
// this is what makes it real. The whole feature is optional: it's only
// wired up (see cmd/server/main.go) when KB_ENABLED=true, since it depends
// on two external stores neither install is required to run.
package knowledgebase

import "time"

// EmbeddingDimension is the fixed vector width every knowledge base's
// registered embedding model must produce — internal/adapter/milvus's
// collection schema pins the vector field to this width, matching what
// OpenAI's ada-002/text-embedding-3-small (and most OpenAI-compatible
// embedding models) produce.
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
