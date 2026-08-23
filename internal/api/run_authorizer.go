package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// storeResourceAuthorizer implements adk.ResourceAuthorizer against the
// real resource-center tables, scoped to one Bundle's owner — spec-10 §2:
// "capabilities.tools 里的每个 ref 都要先经 Resource Registry 校验：存在、
// 未禁用、当前用户有权使用". A disabled or missing ref simply isn't
// authorized (ok=false); only a genuine lookup failure is an error.
type storeResourceAuthorizer struct {
	ctx     context.Context
	q       store.Querier
	ownerID int64
	aesKey  []byte
}

func newStoreResourceAuthorizer(ctx context.Context, q store.Querier, ownerID int64, aesKey []byte) adk.ResourceAuthorizer {
	return &storeResourceAuthorizer{ctx: ctx, q: q, ownerID: ownerID, aesKey: aesKey}
}

func (a *storeResourceAuthorizer) Authorize(_ context.Context, ref string) (adk.ToolSpec, bool, error) {
	// A ref can be a tool, an mcp server, or a knowledge base (all three
	// surface via capabilities.tools[], per the DSL — spec-05) — try each,
	// or a skill (capabilities.skills[]).
	if row, ok, err := a.tryTool(ref); err != nil || ok {
		return row, ok, err
	}
	if row, ok, err := a.tryMCP(ref); err != nil || ok {
		return row, ok, err
	}
	if row, ok, err := a.tryKnowledgeBase(ref); err != nil || ok {
		return row, ok, err
	}
	if row, ok, err := a.trySkill(ref); err != nil || ok {
		return row, ok, err
	}
	return adk.ToolSpec{}, false, nil
}

func (a *storeResourceAuthorizer) tryTool(ref string) (adk.ToolSpec, bool, error) {
	row, err := a.q.GetToolLatestByRef(a.ctx, store.GetToolLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	if row.Status != 1 {
		return adk.ToolSpec{}, false, nil
	}
	cfg, err := a.decryptConfig(row.Config)
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	return adk.ToolSpec{Ref: ref, Kind: adk.KindTool, Config: cfg}, true, nil
}

func (a *storeResourceAuthorizer) tryMCP(ref string) (adk.ToolSpec, bool, error) {
	row, err := a.q.GetMCPServerLatestByRef(a.ctx, store.GetMCPServerLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	if row.Status != 1 {
		return adk.ToolSpec{}, false, nil
	}
	cfg, err := a.decryptConfig(row.Config)
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	return adk.ToolSpec{Ref: ref, Kind: adk.KindMCP, Config: cfg}, true, nil
}

func (a *storeResourceAuthorizer) tryKnowledgeBase(ref string) (adk.ToolSpec, bool, error) {
	row, err := a.q.GetKnowledgeBaseLatestByRef(a.ctx, store.GetKnowledgeBaseLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	if row.Status != 1 {
		return adk.ToolSpec{}, false, nil
	}
	cfg, err := a.decryptConfig(row.Config)
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	// Knowledge bases have no dedicated ADK ResourceKind — surface the
	// same as a plain tool call (they're an endpoint-backed lookup, same
	// as a Tool resource; see spec-05's "surfaces via capabilities.tools").
	return adk.ToolSpec{Ref: ref, Kind: adk.KindTool, Config: cfg}, true, nil
}

func (a *storeResourceAuthorizer) trySkill(ref string) (adk.ToolSpec, bool, error) {
	row, err := a.q.GetSkillLatestByRef(a.ctx, store.GetSkillLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	if row.Status != 1 {
		return adk.ToolSpec{}, false, nil
	}
	cfg, err := a.decryptConfig(row.Config)
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	return adk.ToolSpec{Ref: ref, Kind: adk.KindSkill, Config: cfg}, true, nil
}

func (a *storeResourceAuthorizer) decryptConfig(raw []byte) (map[string]any, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	return DecryptConfigCredentials(a.aesKey, config)
}
