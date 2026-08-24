package orchestrator

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// knowledgeBaseSearcher adapts internal/domain/knowledgebase.Service to
// adk.KnowledgeBaseSearcher — the compiled "knowledge_base" tool's real
// vector-search call.
type knowledgeBaseSearcher struct {
	svc *knowledgebase.Service
}

func newKnowledgeBaseSearcher(svc *knowledgebase.Service) adk.KnowledgeBaseSearcher {
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
