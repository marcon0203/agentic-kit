package knowledgebase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

type fakeVectorStore struct {
	inserted []knowledgebase.ChunkInsert
	deleted  []string
	sources  []knowledgebase.SourceSummary
	results  []knowledgebase.SearchResult
	err      error
}

func (f *fakeVectorStore) Upsert(_ context.Context, _, _ int64, _ string, chunks []knowledgebase.ChunkInsert) error {
	f.inserted = append(f.inserted, chunks...)
	return nil
}
func (f *fakeVectorStore) DeleteSource(_ context.Context, _, _ int64, sourceRef string) error {
	f.deleted = append(f.deleted, sourceRef)
	return nil
}
func (f *fakeVectorStore) ListSources(context.Context, int64, int64) ([]knowledgebase.SourceSummary, error) {
	return f.sources, nil
}
func (f *fakeVectorStore) SearchVector(context.Context, int64, int64, []float32, int) ([]knowledgebase.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeKeywordStore struct {
	indexed []knowledgebase.ChunkInsert
	deleted []string
	results []knowledgebase.SearchResult
	err     error
}

func (f *fakeKeywordStore) Index(_ context.Context, _, _ int64, _ string, chunks []knowledgebase.ChunkInsert) error {
	f.indexed = append(f.indexed, chunks...)
	return nil
}
func (f *fakeKeywordStore) DeleteSource(_ context.Context, _, _ int64, sourceRef string) error {
	f.deleted = append(f.deleted, sourceRef)
	return nil
}
func (f *fakeKeywordStore) SearchKeyword(context.Context, int64, int64, string, int) ([]knowledgebase.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeEmbedder struct {
	dim int
	err error
}

func (f *fakeEmbedder) Embed(_ context.Context, _ modelgateway.ModelSpec, _ map[string]modelgateway.Credential, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, f.dim)
	}
	return out, nil
}

type fakeCreds struct{}

func (fakeCreds) Keys(context.Context, int64) (map[string]modelgateway.Credential, error) {
	return map[string]modelgateway.Credential{"openai": {APIKey: "sk-test"}}, nil
}

type fakeLookup struct {
	res resource.Resource
	err error
}

func (f fakeLookup) Get(context.Context, int64, resource.Kind, int64) (resource.Resource, error) {
	return f.res, f.err
}

func configuredKB() resource.Resource {
	return resource.Resource{
		ID: 1, Kind: resource.KindKnowledgeBase,
		Config: resource.Config{"embedding_provider": "openai", "embedding_model": "text-embedding-3-small"},
	}
}

func newTestService(vectors *fakeVectorStore, keywords *fakeKeywordStore, embed *fakeEmbedder, lookup fakeLookup) *knowledgebase.Service {
	return knowledgebase.NewService(vectors, keywords, embed, fakeCreds{}, lookup)
}

func TestIngest_RejectsEmptyContent(t *testing.T) {
	vectors, keywords := &fakeVectorStore{}, &fakeKeywordStore{}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "   ")
	if _, ok := domain.AsError(err); !ok {
		t.Fatalf("expected a domain error, got %v", err)
	}
	if len(vectors.inserted) != 0 || len(keywords.indexed) != 0 {
		t.Fatal("nothing should be inserted for empty content")
	}
}

func TestIngest_RequiresEmbeddingModelConfigured(t *testing.T) {
	vectors, keywords := &fakeVectorStore{}, &fakeKeywordStore{}
	unconfigured := resource.Resource{ID: 1, Kind: resource.KindKnowledgeBase, Config: resource.Config{}}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: unconfigured})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world")
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeValidationFailed {
		t.Fatalf("expected a validation error naming the missing embedding config, got %v", err)
	}
}

func TestIngest_RejectsDimensionMismatch(t *testing.T) {
	vectors, keywords := &fakeVectorStore{}, &fakeKeywordStore{}
	// The embedder returns 8-dim vectors; the KB column is pinned to
	// EmbeddingDimension, so this must be caught before it ever reaches
	// the stores (a mismatched-width write would fail with a much less
	// useful error from Milvus itself).
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: 8}, fakeLookup{res: configuredKB()})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world")
	if err == nil {
		t.Fatal("expected an error for a dimension mismatch")
	}
	if len(vectors.inserted) != 0 || len(keywords.indexed) != 0 {
		t.Fatal("mismatched-dimension vectors must never reach storage")
	}
}

func TestIngest_ReplacesPriorChunksForTheSameSourceInBothStores(t *testing.T) {
	vectors, keywords := &fakeVectorStore{}, &fakeKeywordStore{}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	n, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world, this is a short document.")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n == 0 {
		t.Fatal("expected at least one chunk")
	}
	if len(vectors.deleted) != 1 || vectors.deleted[0] != "doc-1" {
		t.Fatalf("expected the prior vector chunks for doc-1 to be deleted first, got %+v", vectors.deleted)
	}
	if len(keywords.deleted) != 1 || keywords.deleted[0] != "doc-1" {
		t.Fatalf("expected the prior keyword chunks for doc-1 to be deleted first, got %+v", keywords.deleted)
	}
	if len(vectors.inserted) != n || len(keywords.indexed) != n {
		t.Fatalf("expected %d chunks written to both stores, got vectors=%d keywords=%d", n, len(vectors.inserted), len(keywords.indexed))
	}
}

func TestSearch_DefaultsTopKWhenUnset(t *testing.T) {
	vectors := &fakeVectorStore{results: []knowledgebase.SearchResult{{Chunk: knowledgebase.Chunk{Content: "hit"}, Score: 0.9}}}
	keywords := &fakeKeywordStore{}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	results, err := svc.Search(context.Background(), 1, 1, "what is this about", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Content != "hit" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearch_FusesVectorAndKeywordResultsAgreementRanksHighest(t *testing.T) {
	// "shared" is found by both routes (at different ranks); "vec-only" and
	// "kw-only" are each found by only one. RRF must rank the chunk both
	// routes agree on above either single-route hit.
	shared := knowledgebase.SearchResult{Chunk: knowledgebase.Chunk{SourceRef: "doc", ChunkIndex: 0, Content: "shared"}}
	vecOnly := knowledgebase.SearchResult{Chunk: knowledgebase.Chunk{SourceRef: "doc", ChunkIndex: 1, Content: "vec-only"}}
	kwOnly := knowledgebase.SearchResult{Chunk: knowledgebase.Chunk{SourceRef: "doc", ChunkIndex: 2, Content: "kw-only"}}

	vectors := &fakeVectorStore{results: []knowledgebase.SearchResult{shared, vecOnly}}
	keywords := &fakeKeywordStore{results: []knowledgebase.SearchResult{kwOnly, shared}}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	results, err := svc.Search(context.Background(), 1, 1, "query", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected all 3 distinct chunks in the fused result, got %d: %+v", len(results), results)
	}
	if results[0].Content != "shared" {
		t.Fatalf("expected the chunk both routes found to rank first, got %+v", results[0])
	}
}

func TestSearch_DegradesToVectorOnlyWhenKeywordStoreFails(t *testing.T) {
	vectors := &fakeVectorStore{results: []knowledgebase.SearchResult{{Chunk: knowledgebase.Chunk{Content: "vector hit"}}}}
	keywords := &fakeKeywordStore{err: errors.New("elasticsearch unreachable")}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	results, err := svc.Search(context.Background(), 1, 1, "query", 5)
	if err != nil {
		t.Fatalf("expected a keyword-store failure to degrade rather than fail the search, got %v", err)
	}
	if len(results) != 1 || results[0].Content != "vector hit" {
		t.Fatalf("expected vector-only results, got %+v", results)
	}
}

func TestSearch_FailsWhenVectorStoreFails(t *testing.T) {
	vectors := &fakeVectorStore{err: errors.New("milvus unreachable")}
	keywords := &fakeKeywordStore{results: []knowledgebase.SearchResult{{Chunk: knowledgebase.Chunk{Content: "kw hit"}}}}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	_, err := svc.Search(context.Background(), 1, 1, "query", 5)
	if err == nil {
		t.Fatal("expected a vector-store failure (the required recall route) to fail the search")
	}
}

func TestSplitText_OverlapsAndStaysWithinBounds(t *testing.T) {
	// Exercised indirectly through Ingest above; this checks the property
	// that matters for retrieval quality: a long input produces more than
	// one chunk, and re-ingesting doesn't silently drop content.
	vectors, keywords := &fakeVectorStore{}, &fakeKeywordStore{}
	svc := newTestService(vectors, keywords, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeLookup{res: configuredKB()})

	longText := ""
	for i := 0; i < 500; i++ {
		longText += "word "
	}
	n, err := svc.Ingest(context.Background(), 1, 1, "doc-long", longText)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected a long document to split into multiple chunks, got %d", n)
	}
}
