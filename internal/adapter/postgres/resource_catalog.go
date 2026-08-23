package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ResourceCatalog implements agent.ResourceCatalog.
//
// A capabilities.tools[] entry may name a Tool, an MCP server or a knowledge
// base — three tables. That fan-out is a storage-shape fact, so it is
// resolved here and the Agent context just asks "is this ref usable?".
type ResourceCatalog struct {
	q store.Querier
}

func NewResourceCatalog(q store.Querier) *ResourceCatalog { return &ResourceCatalog{q: q} }

var _ agent.ResourceCatalog = (*ResourceCatalog)(nil)

// latestStatus runs one status lookup, folding "no rows" into found=false
// rather than an error.
func latestStatus(query func() (int16, error)) (agent.RefStatus, error) {
	status, err := query()
	if errors.Is(err, pgx.ErrNoRows) {
		return agent.RefStatus{Found: false}, nil
	}
	if err != nil {
		return agent.RefStatus{}, err
	}
	return agent.RefStatus{Found: true, Enabled: status == int16(agent.StatusEnabled)}, nil
}

func (c *ResourceCatalog) ToolStatus(ctx context.Context, ownerID int64, ref string) (agent.RefStatus, error) {
	lookups := []func() (int16, error){
		func() (int16, error) {
			return c.q.GetToolLatestStatusByRef(ctx, store.GetToolLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
		},
		func() (int16, error) {
			return c.q.GetMCPServerLatestStatusByRef(ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
		},
		func() (int16, error) {
			return c.q.GetKnowledgeBaseLatestStatusByRef(ctx, store.GetKnowledgeBaseLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
		},
	}
	for _, lookup := range lookups {
		status, err := latestStatus(lookup)
		if err != nil {
			return agent.RefStatus{}, err
		}
		if status.Found {
			return status, nil
		}
	}
	return agent.RefStatus{Found: false}, nil
}

func (c *ResourceCatalog) SkillStatus(ctx context.Context, ownerID int64, ref string) (agent.RefStatus, error) {
	return latestStatus(func() (int16, error) {
		return c.q.GetSkillLatestStatusByRef(ctx, store.GetSkillLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
	})
}
