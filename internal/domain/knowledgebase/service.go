package knowledgebase

import (
	"context"
	"errors"
	"fmt"

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

// Repository persists and searches chunks. It never sees a plaintext
// provider credential — only text and vectors.
type Repository interface {
	InsertChunks(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []ChunkInsert) error
	DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error
	ListSources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]SourceSummary, error)
	Search(ctx context.Context, ownerID, knowledgeBaseID int64, queryVector []float32, topK int) ([]SearchResult, error)
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
	repo   Repository
	embed  Embedder
	creds  Credentials
	lookup ResourceLookup
}

func NewService(repo Repository, embed Embedder, creds Credentials, lookup ResourceLookup) *Service {
	return &Service{repo: repo, embed: embed, creds: creds, lookup: lookup}
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

	if err := s.repo.DeleteSource(ctx, ownerID, knowledgeBaseID, sourceRef); err != nil {
		return 0, domain.Internal(err)
	}
	if err := s.repo.InsertChunks(ctx, ownerID, knowledgeBaseID, sourceRef, inserts); err != nil {
		return 0, domain.Internal(err)
	}
	return len(inserts), nil
}

// Sources lists what has been ingested into a knowledge base, one row per
// document rather than per chunk.
func (s *Service) Sources(ctx context.Context, ownerID, knowledgeBaseID int64) ([]SourceSummary, error) {
	rows, err := s.repo.ListSources(ctx, ownerID, knowledgeBaseID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []SourceSummary{}
	}
	return rows, nil
}

// DeleteSource removes every chunk ingested under one source ref.
func (s *Service) DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	if err := s.repo.DeleteSource(ctx, ownerID, knowledgeBaseID, sourceRef); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// defaultTopK is used when a caller (an Agent's tool call, in particular)
// doesn't specify how many chunks it wants back.
const defaultTopK = 5

// Search embeds the query with the knowledge base's own registered model
// and returns its nearest chunks by cosine similarity.
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
	results, err := s.repo.Search(ctx, ownerID, knowledgeBaseID, vectors[0], topK)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}
