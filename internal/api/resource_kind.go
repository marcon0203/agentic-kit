package api

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// resourceRow is the common shape shared by tools/skills/mcp_servers/
// knowledge_bases rows, used so the HTTP layer doesn't need a case per
// concrete sqlc-generated type. Health is the empty string for the three
// kinds that don't have the column.
type resourceRow struct {
	ID          int64
	OwnerUserID int64
	Ref         string
	Version     string
	Config      []byte
	DisplayMeta []byte
	Status      int16
	Health      string
	Immutable   bool
	CreatedAt   pgtype.Timestamptz
}

type agentReference struct {
	OwnerUserID int64
	AgentRef    string
	Version     string
}

// resourceKindStore is implemented once per resource type (tool, skill,
// mcp_server, knowledge_base), each backed by its own table — see
// spec-05-resource-center.md "分表设计" for why they aren't one table.
type resourceKindStore interface {
	Create(ctx context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error)
	GetByID(ctx context.Context, id, ownerID int64) (resourceRow, error)
	ListPage(ctx context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error)
	Update(ctx context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error)
	FindReferencingAgents(ctx context.Context, ownerID int64, ref string) ([]agentReference, error)
}

// ResourceKind names the four registerable resource types, matching
// components.schemas.ResourceType in api/openapi.yaml.
type ResourceKind string

const (
	ResourceKindTool          ResourceKind = "tool"
	ResourceKindSkill         ResourceKind = "skill"
	ResourceKindMCP           ResourceKind = "mcp"
	ResourceKindKnowledgeBase ResourceKind = "knowledge_base"
)

// AllResourceKinds lists every registerable kind, in the order the merged
// (no type filter) list endpoint queries them.
var AllResourceKinds = []ResourceKind{ResourceKindTool, ResourceKindSkill, ResourceKindMCP, ResourceKindKnowledgeBase}

func toAgentReferences[T any](rows []T, get func(T) (int64, string, string)) []agentReference {
	out := make([]agentReference, 0, len(rows))
	for _, r := range rows {
		ownerID, ref, version := get(r)
		out = append(out, agentReference{OwnerUserID: ownerID, AgentRef: ref, Version: version})
	}
	return out
}

// -- tool --

type toolStore struct{ q *store.Queries }

func (s toolStore) Create(ctx context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error) {
	row, err := s.q.CreateTool(ctx, store.CreateToolParams{OwnerUserID: ownerID, Ref: ref, Version: version, Config: config, DisplayMeta: displayMeta})
	return toolRowToResourceRow(row), err
}

func (s toolStore) GetByID(ctx context.Context, id, ownerID int64) (resourceRow, error) {
	row, err := s.q.GetToolByIDForOwner(ctx, store.GetToolByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
	return toolRowToResourceRow(row), err
}

func (s toolStore) ListPage(ctx context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error) {
	rows, err := s.q.ListToolsForOwnerPage(ctx, store.ListToolsForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]resourceRow, len(rows))
	for i, r := range rows {
		out[i] = toolRowToResourceRow(r)
	}
	return out, nil
}

func (s toolStore) Update(ctx context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error) {
	row, err := s.q.UpdateTool(ctx, store.UpdateToolParams{ID: id, OwnerUserID: ownerID, DisplayMeta: displayMeta, Config: config, Status: status})
	return toolRowToResourceRow(row), err
}

func (s toolStore) FindReferencingAgents(ctx context.Context, ownerID int64, ref string) ([]agentReference, error) {
	rows, err := s.q.FindAgentsReferencingToolRef(ctx, store.FindAgentsReferencingToolRefParams{OwnerUserID: ownerID, Column2: ref})
	if err != nil {
		return nil, err
	}
	return toAgentReferences(rows, func(r store.FindAgentsReferencingToolRefRow) (int64, string, string) {
		return r.OwnerUserID, r.AgentRef, r.Version
	}), nil
}

func toolRowToResourceRow(r store.Tool) resourceRow {
	return resourceRow{ID: r.ID, OwnerUserID: r.OwnerUserID, Ref: r.Ref, Version: r.Version, Config: r.Config, DisplayMeta: r.DisplayMeta, Status: r.Status, Immutable: r.Immutable, CreatedAt: r.CreatedAt}
}

// -- skill --

type skillStore struct{ q *store.Queries }

func (s skillStore) Create(ctx context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error) {
	row, err := s.q.CreateSkill(ctx, store.CreateSkillParams{OwnerUserID: ownerID, Ref: ref, Version: version, Config: config, DisplayMeta: displayMeta})
	return skillRowToResourceRow(row), err
}

func (s skillStore) GetByID(ctx context.Context, id, ownerID int64) (resourceRow, error) {
	row, err := s.q.GetSkillByIDForOwner(ctx, store.GetSkillByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
	return skillRowToResourceRow(row), err
}

func (s skillStore) ListPage(ctx context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error) {
	rows, err := s.q.ListSkillsForOwnerPage(ctx, store.ListSkillsForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]resourceRow, len(rows))
	for i, r := range rows {
		out[i] = skillRowToResourceRow(r)
	}
	return out, nil
}

func (s skillStore) Update(ctx context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error) {
	row, err := s.q.UpdateSkill(ctx, store.UpdateSkillParams{ID: id, OwnerUserID: ownerID, DisplayMeta: displayMeta, Config: config, Status: status})
	return skillRowToResourceRow(row), err
}

func (s skillStore) FindReferencingAgents(ctx context.Context, ownerID int64, ref string) ([]agentReference, error) {
	rows, err := s.q.FindAgentsReferencingSkillRef(ctx, store.FindAgentsReferencingSkillRefParams{OwnerUserID: ownerID, Column2: ref})
	if err != nil {
		return nil, err
	}
	return toAgentReferences(rows, func(r store.FindAgentsReferencingSkillRefRow) (int64, string, string) {
		return r.OwnerUserID, r.AgentRef, r.Version
	}), nil
}

func skillRowToResourceRow(r store.Skill) resourceRow {
	return resourceRow{ID: r.ID, OwnerUserID: r.OwnerUserID, Ref: r.Ref, Version: r.Version, Config: r.Config, DisplayMeta: r.DisplayMeta, Status: r.Status, Immutable: r.Immutable, CreatedAt: r.CreatedAt}
}

// -- knowledge base --

type knowledgeBaseStore struct{ q *store.Queries }

func (s knowledgeBaseStore) Create(ctx context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error) {
	row, err := s.q.CreateKnowledgeBase(ctx, store.CreateKnowledgeBaseParams{OwnerUserID: ownerID, Ref: ref, Version: version, Config: config, DisplayMeta: displayMeta})
	return kbRowToResourceRow(row), err
}

func (s knowledgeBaseStore) GetByID(ctx context.Context, id, ownerID int64) (resourceRow, error) {
	row, err := s.q.GetKnowledgeBaseByIDForOwner(ctx, store.GetKnowledgeBaseByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
	return kbRowToResourceRow(row), err
}

func (s knowledgeBaseStore) ListPage(ctx context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error) {
	rows, err := s.q.ListKnowledgeBasesForOwnerPage(ctx, store.ListKnowledgeBasesForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]resourceRow, len(rows))
	for i, r := range rows {
		out[i] = kbRowToResourceRow(r)
	}
	return out, nil
}

func (s knowledgeBaseStore) Update(ctx context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error) {
	row, err := s.q.UpdateKnowledgeBase(ctx, store.UpdateKnowledgeBaseParams{ID: id, OwnerUserID: ownerID, DisplayMeta: displayMeta, Config: config, Status: status})
	return kbRowToResourceRow(row), err
}

func (s knowledgeBaseStore) FindReferencingAgents(ctx context.Context, ownerID int64, ref string) ([]agentReference, error) {
	rows, err := s.q.FindAgentsReferencingKnowledgeBaseRef(ctx, store.FindAgentsReferencingKnowledgeBaseRefParams{OwnerUserID: ownerID, Column2: ref})
	if err != nil {
		return nil, err
	}
	return toAgentReferences(rows, func(r store.FindAgentsReferencingKnowledgeBaseRefRow) (int64, string, string) {
		return r.OwnerUserID, r.AgentRef, r.Version
	}), nil
}

func kbRowToResourceRow(r store.KnowledgeBasis) resourceRow {
	return resourceRow{ID: r.ID, OwnerUserID: r.OwnerUserID, Ref: r.Ref, Version: r.Version, Config: r.Config, DisplayMeta: r.DisplayMeta, Status: r.Status, Immutable: r.Immutable, CreatedAt: r.CreatedAt}
}

// -- mcp server --

type mcpServerStore struct{ q *store.Queries }

func (s mcpServerStore) Create(ctx context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error) {
	row, err := s.q.CreateMCPServer(ctx, store.CreateMCPServerParams{OwnerUserID: ownerID, Ref: ref, Version: version, Config: config, DisplayMeta: displayMeta, Health: "unknown"})
	return mcpRowToResourceRow(row), err
}

func (s mcpServerStore) GetByID(ctx context.Context, id, ownerID int64) (resourceRow, error) {
	row, err := s.q.GetMCPServerByIDForOwner(ctx, store.GetMCPServerByIDForOwnerParams{ID: id, OwnerUserID: ownerID})
	return mcpRowToResourceRow(row), err
}

func (s mcpServerStore) ListPage(ctx context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error) {
	rows, err := s.q.ListMCPServersForOwnerPage(ctx, store.ListMCPServersForOwnerPageParams{OwnerUserID: ownerID, ID: afterID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]resourceRow, len(rows))
	for i, r := range rows {
		out[i] = mcpRowToResourceRow(r)
	}
	return out, nil
}

func (s mcpServerStore) Update(ctx context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error) {
	row, err := s.q.UpdateMCPServer(ctx, store.UpdateMCPServerParams{ID: id, OwnerUserID: ownerID, DisplayMeta: displayMeta, Config: config, Status: status})
	return mcpRowToResourceRow(row), err
}

func (s mcpServerStore) FindReferencingAgents(ctx context.Context, ownerID int64, ref string) ([]agentReference, error) {
	rows, err := s.q.FindAgentsReferencingMCPServerRef(ctx, store.FindAgentsReferencingMCPServerRefParams{OwnerUserID: ownerID, Column2: ref})
	if err != nil {
		return nil, err
	}
	return toAgentReferences(rows, func(r store.FindAgentsReferencingMCPServerRefRow) (int64, string, string) {
		return r.OwnerUserID, r.AgentRef, r.Version
	}), nil
}

func (s mcpServerStore) SetHealth(ctx context.Context, id int64, health string) error {
	return s.q.UpdateMCPServerHealth(ctx, store.UpdateMCPServerHealthParams{ID: id, Health: health})
}

func mcpRowToResourceRow(r store.McpServer) resourceRow {
	return resourceRow{ID: r.ID, OwnerUserID: r.OwnerUserID, Ref: r.Ref, Version: r.Version, Config: r.Config, DisplayMeta: r.DisplayMeta, Status: r.Status, Health: r.Health, Immutable: r.Immutable, CreatedAt: r.CreatedAt}
}
