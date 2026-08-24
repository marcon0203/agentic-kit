package postgres

import (
	"context"

	pgvector "github.com/pgvector/pgvector-go"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// KnowledgeBaseRepository implements knowledgebase.Repository against
// kb_chunks (migration 0013) — the real vector index behind the
// knowledge_base resource kind.
type KnowledgeBaseRepository struct {
	q store.Querier
}

func NewKnowledgeBaseRepository(q store.Querier) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{q: q}
}

var _ knowledgebase.Repository = (*KnowledgeBaseRepository)(nil)

func (r *KnowledgeBaseRepository) InsertChunks(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []knowledgebase.ChunkInsert) error {
	for _, c := range chunks {
		_, err := r.q.InsertKBChunk(ctx, store.InsertKBChunkParams{
			KnowledgeBaseID: knowledgeBaseID,
			OwnerUserID:     ownerID,
			SourceRef:       sourceRef,
			ChunkIndex:      int32(c.ChunkIndex),
			Content:         c.Content,
			Embedding:       pgvector.NewVector(c.Embedding),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *KnowledgeBaseRepository) DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	return r.q.DeleteKBChunksBySource(ctx, store.DeleteKBChunksBySourceParams{
		KnowledgeBaseID: knowledgeBaseID, OwnerUserID: ownerID, SourceRef: sourceRef,
	})
}

func (r *KnowledgeBaseRepository) ListSources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]knowledgebase.SourceSummary, error) {
	rows, err := r.q.ListKBSources(ctx, store.ListKBSourcesParams{KnowledgeBaseID: knowledgeBaseID, OwnerUserID: ownerID})
	if err != nil {
		return nil, err
	}
	out := make([]knowledgebase.SourceSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, knowledgebase.SourceSummary{
			SourceRef: row.SourceRef, ChunkCount: int(row.ChunkCount), IngestedAt: row.IngestedAt.Time,
		})
	}
	return out, nil
}

func (r *KnowledgeBaseRepository) Search(ctx context.Context, ownerID, knowledgeBaseID int64, queryVector []float32, topK int) ([]knowledgebase.SearchResult, error) {
	rows, err := r.q.SearchKBChunks(ctx, store.SearchKBChunksParams{
		KnowledgeBaseID: knowledgeBaseID, OwnerUserID: ownerID,
		Embedding: pgvector.NewVector(queryVector), Limit: int32(topK),
	})
	if err != nil {
		return nil, err
	}
	out := make([]knowledgebase.SearchResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, knowledgebase.SearchResult{
			Chunk: knowledgebase.Chunk{
				ID: row.ID, SourceRef: row.SourceRef, ChunkIndex: int(row.ChunkIndex), Content: row.Content,
			},
			// cosine distance -> similarity: 1 - distance, so higher means
			// more relevant regardless of which distance operator pgvector
			// exposes it as.
			Score: 1 - row.Distance,
		})
	}
	return out, nil
}
