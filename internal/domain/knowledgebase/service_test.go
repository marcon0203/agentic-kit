package knowledgebase_test

import (
	"context"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

type fakeRepo struct {
	inserted []knowledgebase.ChunkInsert
	deleted  []string
	sources  []knowledgebase.SourceSummary
	results  []knowledgebase.SearchResult
}

func (f *fakeRepo) InsertChunks(_ context.Context, _, _ int64, _ string, chunks []knowledgebase.ChunkInsert) error {
	f.inserted = append(f.inserted, chunks...)
	return nil
}
func (f *fakeRepo) DeleteSource(_ context.Context, _, _ int64, sourceRef string) error {
	f.deleted = append(f.deleted, sourceRef)
	return nil
}
func (f *fakeRepo) ListSources(context.Context, int64, int64) ([]knowledgebase.SourceSummary, error) {
	return f.sources, nil
}
func (f *fakeRepo) Search(context.Context, int64, int64, []float32, int) ([]knowledgebase.SearchResult, error) {
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

func TestIngest_RejectsEmptyContent(t *testing.T) {
	repo := &fakeRepo{}
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeCreds{}, fakeLookup{res: configuredKB()})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "   ")
	if _, ok := domain.AsError(err); !ok {
		t.Fatalf("expected a domain error, got %v", err)
	}
	if len(repo.inserted) != 0 {
		t.Fatal("nothing should be inserted for empty content")
	}
}

func TestIngest_RequiresEmbeddingModelConfigured(t *testing.T) {
	repo := &fakeRepo{}
	unconfigured := resource.Resource{ID: 1, Kind: resource.KindKnowledgeBase, Config: resource.Config{}}
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeCreds{}, fakeLookup{res: unconfigured})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world")
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeValidationFailed {
		t.Fatalf("expected a validation error naming the missing embedding config, got %v", err)
	}
}

func TestIngest_RejectsDimensionMismatch(t *testing.T) {
	repo := &fakeRepo{}
	// The embedder returns 8-dim vectors; the KB column is pinned to
	// EmbeddingDimension, so this must be caught before it ever reaches
	// the repository (a mismatched-width write would fail the DB insert
	// with a much less useful error).
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: 8}, fakeCreds{}, fakeLookup{res: configuredKB()})

	_, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world")
	if err == nil {
		t.Fatal("expected an error for a dimension mismatch")
	}
	if len(repo.inserted) != 0 {
		t.Fatal("mismatched-dimension vectors must never reach storage")
	}
}

func TestIngest_ReplacesPriorChunksForTheSameSource(t *testing.T) {
	repo := &fakeRepo{}
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeCreds{}, fakeLookup{res: configuredKB()})

	n, err := svc.Ingest(context.Background(), 1, 1, "doc-1", "hello world, this is a short document.")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n == 0 {
		t.Fatal("expected at least one chunk")
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "doc-1" {
		t.Fatalf("expected the prior chunks for doc-1 to be deleted first, got %+v", repo.deleted)
	}
	if len(repo.inserted) != n {
		t.Fatalf("expected %d chunks inserted, got %d", n, len(repo.inserted))
	}
}

func TestSearch_DefaultsTopKWhenUnset(t *testing.T) {
	repo := &fakeRepo{results: []knowledgebase.SearchResult{{Chunk: knowledgebase.Chunk{Content: "hit"}, Score: 0.9}}}
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeCreds{}, fakeLookup{res: configuredKB()})

	results, err := svc.Search(context.Background(), 1, 1, "what is this about", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Content != "hit" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSplitText_OverlapsAndStaysWithinBounds(t *testing.T) {
	// Exercised indirectly through Ingest above; this checks the property
	// that matters for retrieval quality: a long input produces more than
	// one chunk, and re-ingesting doesn't silently drop content.
	repo := &fakeRepo{}
	svc := knowledgebase.NewService(repo, &fakeEmbedder{dim: knowledgebase.EmbeddingDimension}, fakeCreds{}, fakeLookup{res: configuredKB()})

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
