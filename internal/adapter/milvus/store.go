// Package milvus implements knowledgebase.VectorStore over a Milvus
// collection — the vector-search leg of 多路召回. One shared collection
// ("kb_chunks") holds every knowledge base's chunks; owner_id and
// knowledge_base_id scalar fields do the per-KB/per-owner filtering via a
// boolean expression at query time, mirroring how the removed pgvector
// table used to scope rows by the same two columns.
package milvus

import (
	"context"
	"fmt"
	"time"

	mvclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
)

const (
	collectionName  = "kb_chunks"
	vectorField     = "embedding"
	sourceRefMaxLen = 255
	contentMaxLen   = 8192
)

// Config is Milvus connection settings — mirrors internal/config.Config's
// MILVUS_* fields so main.go can pass them straight through.
type Config struct {
	Addr     string
	Username string
	Password string
}

type Store struct {
	client mvclient.Client
}

var _ knowledgebase.VectorStore = (*Store)(nil)

// NewStore connects to Milvus and ensures the shared kb_chunks collection
// exists, is indexed, and is loaded into memory for search — every step
// here is idempotent, so this is safe to call on every server startup
// rather than needing a separate one-time migration step.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	var c mvclient.Client
	var err error
	if cfg.Username != "" {
		c, err = mvclient.NewDefaultGrpcClientWithAuth(ctx, cfg.Addr, cfg.Username, cfg.Password)
	} else {
		c, err = mvclient.NewDefaultGrpcClient(ctx, cfg.Addr)
	}
	if err != nil {
		return nil, fmt.Errorf("milvus: connect to %s: %w", cfg.Addr, err)
	}

	s := &Store{client: c}
	if err := s.ensureCollection(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.client.Close() }

func (s *Store) ensureCollection(ctx context.Context) error {
	has, err := s.client.HasCollection(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("milvus: check collection: %w", err)
	}
	if !has {
		schema := entity.NewSchema().
			WithName(collectionName).
			WithDescription("agentic-kit 知识库分片 — one collection shared across every knowledge base").
			WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeInt64).WithIsPrimaryKey(true).WithIsAutoID(true)).
			WithField(entity.NewField().WithName("owner_id").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName("knowledge_base_id").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName("source_ref").WithDataType(entity.FieldTypeVarChar).WithMaxLength(sourceRefMaxLen)).
			WithField(entity.NewField().WithName("chunk_index").WithDataType(entity.FieldTypeInt32)).
			WithField(entity.NewField().WithName("content").WithDataType(entity.FieldTypeVarChar).WithMaxLength(contentMaxLen)).
			WithField(entity.NewField().WithName("created_at").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName(vectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(knowledgebase.EmbeddingDimension))

		if err := s.client.CreateCollection(ctx, schema, 2); err != nil {
			return fmt.Errorf("milvus: create collection: %w", err)
		}
		idx, err := entity.NewIndexAUTOINDEX(entity.COSINE)
		if err != nil {
			return fmt.Errorf("milvus: build index params: %w", err)
		}
		if err := s.client.CreateIndex(ctx, collectionName, vectorField, idx, false); err != nil {
			return fmt.Errorf("milvus: create index: %w", err)
		}
	}
	if err := s.client.LoadCollection(ctx, collectionName, false); err != nil {
		return fmt.Errorf("milvus: load collection: %w", err)
	}
	return nil
}

func scopeExpr(ownerID, kbID int64) string {
	return fmt.Sprintf("owner_id == %d && knowledge_base_id == %d", ownerID, kbID)
}

func sourceExpr(ownerID, kbID int64, sourceRef string) string {
	return fmt.Sprintf("%s && source_ref == %q", scopeExpr(ownerID, kbID), sourceRef)
}

func (s *Store) Upsert(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []knowledgebase.ChunkInsert) error {
	if len(chunks) == 0 {
		return nil
	}
	n := len(chunks)
	ownerIDs := make([]int64, n)
	kbIDs := make([]int64, n)
	refs := make([]string, n)
	indices := make([]int32, n)
	contents := make([]string, n)
	createdAts := make([]int64, n)
	vectors := make([][]float32, n)
	now := time.Now().Unix()
	for i, c := range chunks {
		ownerIDs[i] = ownerID
		kbIDs[i] = knowledgeBaseID
		refs[i] = sourceRef
		indices[i] = int32(c.ChunkIndex)
		contents[i] = c.Content
		createdAts[i] = now
		vectors[i] = c.Embedding
	}

	_, err := s.client.Insert(ctx, collectionName, "",
		entity.NewColumnInt64("owner_id", ownerIDs),
		entity.NewColumnInt64("knowledge_base_id", kbIDs),
		entity.NewColumnVarChar("source_ref", refs),
		entity.NewColumnInt32("chunk_index", indices),
		entity.NewColumnVarChar("content", contents),
		entity.NewColumnInt64("created_at", createdAts),
		entity.NewColumnFloatVector(vectorField, knowledgebase.EmbeddingDimension, vectors),
	)
	if err != nil {
		return fmt.Errorf("milvus: insert: %w", err)
	}
	if err := s.client.Flush(ctx, collectionName, false); err != nil {
		return fmt.Errorf("milvus: flush: %w", err)
	}
	return nil
}

func (s *Store) DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	if err := s.client.Delete(ctx, collectionName, "", sourceExpr(ownerID, knowledgeBaseID, sourceRef)); err != nil {
		return fmt.Errorf("milvus: delete: %w", err)
	}
	return nil
}

// ListSources aggregates chunks into one row per source_ref client-side —
// this SDK's Query has no server-side GROUP BY, and a knowledge base's
// document count is small enough (documents an owner registered by hand,
// not a firehose) that pulling every chunk's source_ref/created_at and
// folding them in Go is the simplest correct option.
func (s *Store) ListSources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]knowledgebase.SourceSummary, error) {
	rs, err := s.client.Query(ctx, collectionName, nil, scopeExpr(ownerID, knowledgeBaseID), []string{"source_ref", "created_at"})
	if err != nil {
		return nil, fmt.Errorf("milvus: query sources: %w", err)
	}
	refCol := rs.GetColumn("source_ref")
	tsCol := rs.GetColumn("created_at")
	if refCol == nil {
		return nil, nil
	}

	type agg struct {
		count int
		maxTS int64
	}
	byRef := map[string]*agg{}
	order := make([]string, 0)
	for i := 0; i < refCol.Len(); i++ {
		ref, err := refCol.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("milvus: decode source_ref: %w", err)
		}
		var ts int64
		if tsCol != nil {
			ts, _ = tsCol.GetAsInt64(i)
		}
		a, ok := byRef[ref]
		if !ok {
			a = &agg{}
			byRef[ref] = a
			order = append(order, ref)
		}
		a.count++
		if ts > a.maxTS {
			a.maxTS = ts
		}
	}

	out := make([]knowledgebase.SourceSummary, 0, len(order))
	for _, ref := range order {
		a := byRef[ref]
		out = append(out, knowledgebase.SourceSummary{
			SourceRef: ref, ChunkCount: a.count, IngestedAt: time.Unix(a.maxTS, 0).UTC(),
		})
	}
	return out, nil
}

func (s *Store) SearchVector(ctx context.Context, ownerID, knowledgeBaseID int64, queryVector []float32, topK int) ([]knowledgebase.SearchResult, error) {
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, fmt.Errorf("milvus: search param: %w", err)
	}
	results, err := s.client.Search(ctx, collectionName, nil, scopeExpr(ownerID, knowledgeBaseID),
		[]string{"source_ref", "chunk_index", "content"},
		[]entity.Vector{entity.FloatVector(queryVector)},
		vectorField, entity.COSINE, topK, sp)
	if err != nil {
		return nil, fmt.Errorf("milvus: search: %w", err)
	}
	if len(results) == 0 || results[0].ResultCount == 0 {
		return nil, nil
	}

	r := results[0]
	refCol := r.Fields.GetColumn("source_ref")
	idxCol := r.Fields.GetColumn("chunk_index")
	contentCol := r.Fields.GetColumn("content")
	out := make([]knowledgebase.SearchResult, 0, r.ResultCount)
	for i := 0; i < r.ResultCount; i++ {
		ref, err := refCol.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("milvus: decode source_ref: %w", err)
		}
		idx64, err := idxCol.GetAsInt64(i)
		if err != nil {
			return nil, fmt.Errorf("milvus: decode chunk_index: %w", err)
		}
		content, err := contentCol.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("milvus: decode content: %w", err)
		}
		// COSINE is a similarity metric in Milvus (not a distance), so
		// Scores[i] is already "higher is better" — no sign flip needed,
		// unlike the pgvector `1 - cosine_distance` this replaces.
		out = append(out, knowledgebase.SearchResult{
			Chunk: knowledgebase.Chunk{SourceRef: ref, ChunkIndex: int(idx64), Content: content},
			Score: float64(r.Scores[i]),
		})
	}
	return out, nil
}
