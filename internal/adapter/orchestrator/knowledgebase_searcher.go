package orchestrator

import (
	"context"
	"errors"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// knowledgeBaseSearcher adapts internal/domain/knowledgebase.Service to
// adk.KnowledgeBaseSearcher — the compiled "knowledge_base" tool's real
// hybrid (Milvus + Elasticsearch) search call.
type knowledgeBaseSearcher struct {
	svc *knowledgebase.Service
}

// newKnowledgeBaseSearcher returns a disabledKnowledgeBaseSearcher when svc
// is nil — the whole 知识库 feature is optional (KB_ENABLED), and an Agent
// whose DSL still references a knowledge_base tool needs a clear rejection
// here rather than a nil-pointer panic reaching into the run.
func newKnowledgeBaseSearcher(svc *knowledgebase.Service) adk.KnowledgeBaseSearcher {
	if svc == nil {
		return disabledKnowledgeBaseSearcher{}
	}
	return &knowledgeBaseSearcher{svc: svc}
}

func (s *knowledgeBaseSearcher) Search(ctx context.Context, ownerID, knowledgeBaseID int64, query string, topK int) ([]adk.KnowledgeBaseSearchResult, error) {
	results, err := s.svc.Search(ctx, ownerID, knowledgeBaseID, query, topK)
	if err != nil {
		return nil, err
	}
	out := make([]adk.KnowledgeBaseSearchResult, len(results))
	for i, r := range results {
		out[i] = adk.KnowledgeBaseSearchResult{SourceRef: r.SourceRef, Content: r.Content, Score: r.Score}
	}
	return out, nil
}

type disabledKnowledgeBaseSearcher struct{}

func (disabledKnowledgeBaseSearcher) Search(context.Context, int64, int64, string, int) ([]adk.KnowledgeBaseSearchResult, error) {
	return nil, errors.New("知识库功能未启用（KB_ENABLED=false），这个 Agent 引用的 knowledge_base 工具无法使用")
}
