package knowledgebase

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

// ErrNoEmbeddingModel means the knowledge_base resource was registered
// without an embedding_provider/embedding_model in its config — there is
// nothing to embed with, so ingest/search can't proceed.
var ErrNoEmbeddingModel = errors.New("knowledgebase: no embedding_provider/embedding_model configured")

// ChunkInsert is one chunk ready to persist, already embedded.
type ChunkInsert struct {
	ChunkIndex int
	Content    string
	Embedding  []float32
}

// VectorStore is the Milvus-backed leg of 多路召回 (multi-route recall):
// nearest-neighbor search over chunk embeddings. It never sees a plaintext
// provider credential — only text and vectors. It also doubles as the
// source of truth for "what has been ingested" (ListSources) since every
// ingest writes chunks here first.
type VectorStore interface {
	Upsert(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []ChunkInsert) error
	DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error
	ListSources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]SourceSummary, error)
	SearchVector(ctx context.Context, ownerID, knowledgeBaseID int64, queryVector []float32, topK int) ([]SearchResult, error)
}

// KeywordStore is the Elasticsearch-backed leg of 多路召回: BM25 keyword
// search over the same chunks' raw text. This is what still surfaces a
// chunk a pure embedding search would miss — an exact product code, an
// acronym, a name that doesn't carry much semantic weight but matters a
// lot lexically.
type KeywordStore interface {
	Index(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []ChunkInsert) error
	DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error
	SearchKeyword(ctx context.Context, ownerID, knowledgeBaseID int64, query string, topK int) ([]SearchResult, error)
}

// Embedder calls whichever model provider turns text into vectors.
// *modelgateway.Gateway satisfies this directly.
type Embedder interface {
	Embed(ctx context.Context, spec modelgateway.ModelSpec, creds map[string]modelgateway.Credential, texts []string) ([][]float32, error)
}

// Credentials supplies the caller's decrypted provider keys.
// *postgres.ProviderKeyStore satisfies this directly.
type Credentials interface {
	Keys(ctx context.Context, ownerID int64) (map[string]modelgateway.Credential, error)
}

// ResourceLookup reads a knowledge_base resource's own config (which
// embedding provider/model it's registered against) and enforces
// ownership. *resource.Service satisfies this directly.
type ResourceLookup interface {
	Get(ctx context.Context, ownerID int64, kind resource.Kind, id int64) (resource.Resource, error)
}

// Service is the 知识库 retrieval application service.
type Service struct {
	vectors  VectorStore
	keywords KeywordStore
	embed    Embedder
	creds    Credentials
	lookup   ResourceLookup
}

func NewService(vectors VectorStore, keywords KeywordStore, embed Embedder, creds Credentials, lookup ResourceLookup) *Service {
	return &Service{vectors: vectors, keywords: keywords, embed: embed, creds: creds, lookup: lookup}
}

func embeddingSpec(res resource.Resource) (modelgateway.ModelSpec, error) {
	provider, _ := res.Config["embedding_provider"].(string)
	model, _ := res.Config["embedding_model"].(string)
	if provider == "" || model == "" {
		return modelgateway.ModelSpec{}, ErrNoEmbeddingModel
	}
	return modelgateway.ModelSpec{Provider: provider, Name: model}, nil
}

func (s *Service) resolve(ctx context.Context, ownerID, knowledgeBaseID int64) (modelgateway.ModelSpec, map[string]modelgateway.Credential, error) {
	// ResourceLookup.Get already returns a proper domain.Error (NotFound
	// included) — propagated as-is rather than re-wrapped, since
	// resource.Service.Get is the one place that decides what "not found"
	// looks like to a caller.
	res, err := s.lookup.Get(ctx, ownerID, resource.KindKnowledgeBase, knowledgeBaseID)
	if err != nil {
		return modelgateway.ModelSpec{}, nil, err
	}
	spec, err := embeddingSpec(res)
	if err != nil {
		return modelgateway.ModelSpec{}, nil, domain.Invalid(domain.CodeValidationFailed, err.Error())
	}
	creds, err := s.creds.Keys(ctx, ownerID)
	if err != nil {
		return modelgateway.ModelSpec{}, nil, domain.Internal(err)
	}
	return spec, creds, nil
}

// Ingest chunks, embeds and stores one document's text under sourceRef,
// replacing whatever was previously ingested under that same ref —
// re-ingesting a source is how its content gets updated.
func (s *Service) Ingest(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef, text string) (int, error) {
	if sourceRef == "" {
		return 0, domain.Invalid(domain.CodeValidationFailed, "source_ref is required")
	}
	chunks := splitText(text, chunkSize, chunkOverlap)
	if len(chunks) == 0 {
		return 0, domain.Invalid(domain.CodeValidationFailed, "content is empty")
	}

	spec, creds, err := s.resolve(ctx, ownerID, knowledgeBaseID)
	if err != nil {
		return 0, err
	}

	vectors, err := s.embed.Embed(ctx, spec, creds, chunks)
	if err != nil {
		return 0, domain.Unprocessable(domain.CodeProviderCredsInvalid, "embedding 调用失败："+err.Error())
	}

	inserts := make([]ChunkInsert, len(chunks))
	for i, c := range chunks {
		if len(vectors[i]) != EmbeddingDimension {
			return 0, domain.Unprocessable(domain.CodeValidationFailed,
				fmt.Sprintf("embedding 模型输出 %d 维，知识库要求 %d 维", len(vectors[i]), EmbeddingDimension))
		}
		inserts[i] = ChunkInsert{ChunkIndex: i, Content: c, Embedding: vectors[i]}
	}

	// Re-ingesting a source clears it from both stores before writing the
	// fresh chunks, so a document that shrank doesn't leave stale trailing
	// chunks behind in either index.
	if err := s.deleteFromBothStores(ctx, ownerID, knowledgeBaseID, sourceRef); err != nil {
		return 0, err
	}
	if err := s.vectors.Upsert(ctx, ownerID, knowledgeBaseID, sourceRef, inserts); err != nil {
		return 0, domain.Internal(err)
	}
	if err := s.keywords.Index(ctx, ownerID, knowledgeBaseID, sourceRef, inserts); err != nil {
		return 0, domain.Internal(err)
	}
	return len(inserts), nil
}

// Sources lists what has been ingested into a knowledge base, one row per
// document rather than per chunk. Read from the vector store — every
// ingest writes there first, so it's the source of truth for "what
// exists" (the keyword store never needs its own listing path).
func (s *Service) Sources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]SourceSummary, error) {
	rows, err := s.vectors.ListSources(ctx, ownerID, knowledgeBaseID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []SourceSummary{}
	}
	return rows, nil
}

// DeleteSource removes every chunk ingested under one source ref, from
// both stores.
func (s *Service) DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	return s.deleteFromBothStores(ctx, ownerID, knowledgeBaseID, sourceRef)
}

func (s *Service) deleteFromBothStores(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	if err := s.vectors.DeleteSource(ctx, ownerID, knowledgeBaseID, sourceRef); err != nil {
		return domain.Internal(err)
	}
	if err := s.keywords.DeleteSource(ctx, ownerID, knowledgeBaseID, sourceRef); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// defaultTopK is used when a caller (an Agent's tool call, in particular)
// doesn't specify how many chunks it wants back.
const defaultTopK = 5

// Search is 多路召回: it embeds the query and runs Milvus vector search and
// Elasticsearch keyword search in parallel, then merges both ranked lists
// with Reciprocal Rank Fusion (fuseRRF). Vector search is the required
// signal — its failure fails the whole call, since the embedding call
// already succeeded and a vector-store outage means retrieval is actually
// broken. Keyword search is the supplementary signal: if Elasticsearch
// errors, Search degrades to vector-only results rather than failing the
// entire query over an outage in the secondary recall route.
func (s *Service) Search(ctx context.Context, ownerID, knowledgeBaseID int64, query string, topK int) ([]SearchResult, error) {
	spec, creds, err := s.resolve(ctx, ownerID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	vectors, err := s.embed.Embed(ctx, spec, creds, []string{query})
	if err != nil {
		return nil, domain.Unprocessable(domain.CodeProviderCredsInvalid, "embedding 调用失败："+err.Error())
	}
	if topK <= 0 {
		topK = defaultTopK
	}

	var vecResults, kwResults []SearchResult
	var vecErr, kwErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		vecResults, vecErr = s.vectors.SearchVector(ctx, ownerID, knowledgeBaseID, vectors[0], topK)
	}()
	go func() {
		defer wg.Done()
		kwResults, kwErr = s.keywords.SearchKeyword(ctx, ownerID, knowledgeBaseID, query, topK)
	}()
	wg.Wait()

	if vecErr != nil {
		return nil, domain.Internal(vecErr)
	}
	if kwErr != nil {
		kwResults = nil
	}

	results := fuseRRF(topK, vecResults, kwResults)
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}
