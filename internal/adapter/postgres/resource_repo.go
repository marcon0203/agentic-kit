package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ResourceRepository implements resource.Repository. The four kinds live in
// four tables (spec-05 "分表设计"), so every method dispatches on Kind — that
// fan-out is exactly the storage detail the port exists to hide, and it now
// lives here instead of in a map of stores held by the HTTP handler.
type ResourceRepository struct{ q store.Querier }

func NewResourceRepository(q store.Querier) *ResourceRepository { return &ResourceRepository{q: q} }

var _ resource.Repository = (*ResourceRepository)(nil)

// marshalResource renders a domain Resource back into the two JSON columns.
func marshalResource(r resource.Resource) (config, displayMeta []byte, err error) {
	config, err = json.Marshal(r.Config)
	if err != nil {
		return nil, nil, err
	}
	displayMeta, err = json.Marshal(map[string]any{"display_name": r.DisplayName})
	if err != nil {
		return nil, nil, err
	}
	return config, displayMeta, nil
}

func (r *ResourceRepository) Create(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	config, displayMeta, err := marshalResource(res)
	if err != nil {
		return resource.Resource{}, err
	}

	var out resource.Resource
	switch res.Kind {
	case resource.KindTool:
		got, e := r.q.CreateTool(ctx, store.CreateToolParams{OwnerUserID: res.OwnerID, Ref: res.Ref, Version: res.Version, Config: config, DisplayMeta: displayMeta})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		out = fromTool(got)
	case resource.KindSkill:
		got, e := r.q.CreateSkill(ctx, store.CreateSkillParams{OwnerUserID: res.OwnerID, Ref: res.Ref, Version: res.Version, Config: config, DisplayMeta: displayMeta})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		out = fromSkill(got)
	case resource.KindKnowledgeBase:
		got, e := r.q.CreateKnowledgeBase(ctx, store.CreateKnowledgeBaseParams{OwnerUserID: res.OwnerID, Ref: res.Ref, Version: res.Version, Config: config, DisplayMeta: displayMeta})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		out = fromKB(got)
	case resource.KindMCP:
		got, e := r.q.CreateMCPServer(ctx, store.CreateMCPServerParams{OwnerUserID: res.OwnerID, Ref: res.Ref, Version: res.Version, Config: config, DisplayMeta: displayMeta, Health: "unknown"})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		out = fromMCP(got)
	case resource.KindMemory:
		got, e := r.q.CreateMemory(ctx, store.CreateMemoryParams{OwnerUserID: res.OwnerID, Ref: res.Ref, Version: res.Version, Config: config, DisplayMeta: displayMeta})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		out = fromMemory(got)
	default:
		return resource.Resource{}, resource.ErrNotFound
	}
	return out, nil
}

func (r *ResourceRepository) GetByID(ctx context.Context, kind resource.Kind, id, ownerID int64) (resource.Resource, error) {
	switch kind {
	case resource.KindTool:
		got, err := r.q.GetToolByIDForOwner(ctx, store.GetToolByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
		if err != nil {
			return resource.Resource{}, translateResourceErr(err)
		}
		return fromTool(got), nil
	case resource.KindSkill:
		got, err := r.q.GetSkillByIDForOwner(ctx, store.GetSkillByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
		if err != nil {
			return resource.Resource{}, translateResourceErr(err)
		}
		return fromSkill(got), nil
	case resource.KindKnowledgeBase:
		got, err := r.q.GetKnowledgeBaseByIDForOwner(ctx, store.GetKnowledgeBaseByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
		if err != nil {
			return resource.Resource{}, translateResourceErr(err)
		}
		return fromKB(got), nil
	case resource.KindMCP:
		got, err := r.q.GetMCPServerByIDForOwner(ctx, store.GetMCPServerByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
		if err != nil {
			return resource.Resource{}, translateResourceErr(err)
		}
		return fromMCP(got), nil
	case resource.KindMemory:
		got, err := r.q.GetMemoryByIDForOwner(ctx, store.GetMemoryByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
		if err != nil {
			return resource.Resource{}, translateResourceErr(err)
		}
		return fromMemory(got), nil
	default:
		return resource.Resource{}, resource.ErrNotFound
	}
}

func (r *ResourceRepository) ListPage(ctx context.Context, kind resource.Kind, ownerID, afterID int64, limit int32) ([]resource.Resource, error) {
	switch kind {
	case resource.KindTool:
		rows, err := r.q.ListToolsForOwnerPage(ctx, store.ListToolsForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		out := make([]resource.Resource, len(rows))
		for i, x := range rows {
			out[i] = fromTool(x)
		}
		return out, nil
	case resource.KindSkill:
		rows, err := r.q.ListSkillsForOwnerPage(ctx, store.ListSkillsForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		out := make([]resource.Resource, len(rows))
		for i, x := range rows {
			out[i] = fromSkill(x)
		}
		return out, nil
	case resource.KindKnowledgeBase:
		rows, err := r.q.ListKnowledgeBasesForOwnerPage(ctx, store.ListKnowledgeBasesForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		out := make([]resource.Resource, len(rows))
		for i, x := range rows {
			out[i] = fromKB(x)
		}
		return out, nil
	case resource.KindMCP:
		rows, err := r.q.ListMCPServersForOwnerPage(ctx, store.ListMCPServersForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		out := make([]resource.Resource, len(rows))
		for i, x := range rows {
			out[i] = fromMCP(x)
		}
		return out, nil
	case resource.KindMemory:
		rows, err := r.q.ListMemoriesForOwnerPage(ctx, store.ListMemoriesForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		out := make([]resource.Resource, len(rows))
		for i, x := range rows {
			out[i] = fromMemory(x)
		}
		return out, nil
	default:
		return nil, nil
	}
}

func (r *ResourceRepository) Update(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	config, displayMeta, err := marshalResource(res)
	if err != nil {
		return resource.Resource{}, err
	}
	status := int16(res.Status)

	switch res.Kind {
	case resource.KindTool:
		got, e := r.q.UpdateTool(ctx, store.UpdateToolParams{ID: res.ID, OwnerUserID: res.OwnerID, DisplayMeta: displayMeta, Config: config, Status: status})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		return fromTool(got), nil
	case resource.KindSkill:
		got, e := r.q.UpdateSkill(ctx, store.UpdateSkillParams{ID: res.ID, OwnerUserID: res.OwnerID, DisplayMeta: displayMeta, Config: config, Status: status})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		return fromSkill(got), nil
	case resource.KindKnowledgeBase:
		got, e := r.q.UpdateKnowledgeBase(ctx, store.UpdateKnowledgeBaseParams{ID: res.ID, OwnerUserID: res.OwnerID, DisplayMeta: displayMeta, Config: config, Status: status})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		return fromKB(got), nil
	case resource.KindMCP:
		got, e := r.q.UpdateMCPServer(ctx, store.UpdateMCPServerParams{ID: res.ID, OwnerUserID: res.OwnerID, DisplayMeta: displayMeta, Config: config, Status: status})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		return fromMCP(got), nil
	case resource.KindMemory:
		got, e := r.q.UpdateMemory(ctx, store.UpdateMemoryParams{ID: res.ID, OwnerUserID: res.OwnerID, DisplayMeta: displayMeta, Config: config, Status: status})
		if e != nil {
			return resource.Resource{}, translateResourceErr(e)
		}
		return fromMemory(got), nil
	default:
		return resource.Resource{}, resource.ErrNotFound
	}
}

func (r *ResourceRepository) FindReferencingAgents(ctx context.Context, kind resource.Kind, ownerID int64, ref string) ([]resource.AgentReference, error) {
	switch kind {
	case resource.KindTool:
		rows, err := r.q.FindAgentsReferencingToolRef(ctx, store.FindAgentsReferencingToolRefParams{OwnerUserID: ownerID, Column2: ref})
		if err != nil {
			return nil, err
		}
		out := make([]resource.AgentReference, len(rows))
		for i, x := range rows {
			out[i] = resource.AgentReference{AgentRef: x.AgentRef, Version: x.Version}
		}
		return out, nil
	case resource.KindSkill:
		rows, err := r.q.FindAgentsReferencingSkillRef(ctx, store.FindAgentsReferencingSkillRefParams{OwnerUserID: ownerID, Column2: ref})
		if err != nil {
			return nil, err
		}
		out := make([]resource.AgentReference, len(rows))
		for i, x := range rows {
			out[i] = resource.AgentReference{AgentRef: x.AgentRef, Version: x.Version}
		}
		return out, nil
	case resource.KindKnowledgeBase:
		rows, err := r.q.FindAgentsReferencingKnowledgeBaseRef(ctx, store.FindAgentsReferencingKnowledgeBaseRefParams{OwnerUserID: ownerID, Column2: ref})
		if err != nil {
			return nil, err
		}
		out := make([]resource.AgentReference, len(rows))
		for i, x := range rows {
			out[i] = resource.AgentReference{AgentRef: x.AgentRef, Version: x.Version}
		}
		return out, nil
	case resource.KindMCP:
		rows, err := r.q.FindAgentsReferencingMCPServerRef(ctx, store.FindAgentsReferencingMCPServerRefParams{OwnerUserID: ownerID, Column2: ref})
		if err != nil {
			return nil, err
		}
		out := make([]resource.AgentReference, len(rows))
		for i, x := range rows {
			out[i] = resource.AgentReference{AgentRef: x.AgentRef, Version: x.Version}
		}
		return out, nil
	case resource.KindMemory:
		// A memory resource is never referenced from an Agent's
		// capabilities.tools/skills[] (it's wired in at the run level, not
		// per-Agent — see internal/adapter/orchestrator's run engine), so
		// it's always safe to delete.
		return nil, nil
	default:
		return nil, nil
	}
}

// SetHealth is MCP-only; the other three tables have no health column.
func (r *ResourceRepository) SetHealth(ctx context.Context, id int64, health resource.Health) error {
	return r.q.UpdateMCPServerHealth(ctx, store.UpdateMCPServerHealthParams{ID: id, Health: string(health)})
}

// translateResourceErr maps storage signals onto this context's sentinels.
func translateResourceErr(err error) error {
	switch {
	case err == nil:
		return nil
	case isUniqueViolation(err):
		return resource.ErrDuplicate
	case errors.Is(err, pgx.ErrNoRows):
		return resource.ErrNotFound
	default:
		return err
	}
}

// Row converters — one per table, all producing the same domain type. The
// JSON columns are decoded here so the domain never handles []byte.

func decodeConfig(raw []byte) resource.Config {
	var cfg resource.Config
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

func decodeDisplayName(raw []byte) string {
	var meta map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	name, _ := meta["display_name"].(string)
	return name
}

func fromTool(x store.Tool) resource.Resource {
	return resource.Resource{
		ID: x.ID, OwnerID: x.OwnerUserID, Kind: resource.KindTool, Ref: x.Ref, Version: x.Version,
		DisplayName: decodeDisplayName(x.DisplayMeta), Config: decodeConfig(x.Config),
		Status: resource.Status(x.Status), CreatedAt: x.CreatedAt.Time,
	}
}

func fromSkill(x store.Skill) resource.Resource {
	return resource.Resource{
		ID: x.ID, OwnerID: x.OwnerUserID, Kind: resource.KindSkill, Ref: x.Ref, Version: x.Version,
		DisplayName: decodeDisplayName(x.DisplayMeta), Config: decodeConfig(x.Config),
		Status: resource.Status(x.Status), CreatedAt: x.CreatedAt.Time,
	}
}

func fromKB(x store.KnowledgeBasis) resource.Resource {
	return resource.Resource{
		ID: x.ID, OwnerID: x.OwnerUserID, Kind: resource.KindKnowledgeBase, Ref: x.Ref, Version: x.Version,
		DisplayName: decodeDisplayName(x.DisplayMeta), Config: decodeConfig(x.Config),
		Status: resource.Status(x.Status), CreatedAt: x.CreatedAt.Time,
	}
}

func fromMCP(x store.McpServer) resource.Resource {
	return resource.Resource{
		ID: x.ID, OwnerID: x.OwnerUserID, Kind: resource.KindMCP, Ref: x.Ref, Version: x.Version,
		DisplayName: decodeDisplayName(x.DisplayMeta), Config: decodeConfig(x.Config),
		Status: resource.Status(x.Status), Health: resource.Health(x.Health), CreatedAt: x.CreatedAt.Time,
	}
}

func fromMemory(x store.Memory) resource.Resource {
	return resource.Resource{
		ID: x.ID, OwnerID: x.OwnerUserID, Kind: resource.KindMemory, Ref: x.Ref, Version: x.Version,
		DisplayName: decodeDisplayName(x.DisplayMeta), Config: decodeConfig(x.Config),
		Status: resource.Status(x.Status), CreatedAt: x.CreatedAt.Time,
	}
}
