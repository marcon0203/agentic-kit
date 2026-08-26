package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// PluginToolResolver resolves one capabilities.tools[] entry shaped like a
// plugin ref ("plugin:{plugin_id}/{name}") against the caller's own
// installations — implemented by plugin.Service.ToolRefStatus. ok=false
// means ref isn't a plugin ref at all, so ResourceCatalog should fall
// through to its ordinary tool/mcp/knowledge-base lookups instead.
type PluginToolResolver interface {
	ToolRefStatus(ctx context.Context, ownerID int64, ref string) (found, enabled, ok bool, err error)
}

// ResourceCatalog implements agent.ResourceCatalog.
//
// A capabilities.tools[] entry may name a Tool, an MCP server, a knowledge
// base, or — since spec-20 — a plugin's tool/renderer. That fan-out is a
// storage-shape fact, so it is resolved here and the Agent context just
// asks "is this ref usable?". plugins is wired in after construction
// (SetPluginResolver) rather than through the constructor: plugin.Service
// itself depends on things assembled later in main.go's wiring order, and
// every caller of NewResourceCatalog already holds this same pointer, so
// the later mutation is visible to all of them.
type ResourceCatalog struct {
	q       store.Querier
	plugins PluginToolResolver
}

func NewResourceCatalog(q store.Querier) *ResourceCatalog { return &ResourceCatalog{q: q} }

// SetPluginResolver wires in plugin ref resolution once plugin.Service
// exists. Nil (the zero value, before this is called) means plugin: refs
// always resolve to not-found — the pre-spec-20 behavior.
func (c *ResourceCatalog) SetPluginResolver(p PluginToolResolver) { c.plugins = p }

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
	if c.plugins != nil {
		found, enabled, ok, err := c.plugins.ToolRefStatus(ctx, ownerID, ref)
		if err != nil {
			return agent.RefStatus{}, err
		}
		if ok {
			return agent.RefStatus{Found: found, Enabled: enabled}, nil
		}
	}

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
