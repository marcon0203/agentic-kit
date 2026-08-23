package orchestrator

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	adaptercrypto "github.com/marcon0203/agentic-kit/internal/adapter/crypto"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// resourceAuthorizer implements adk.ResourceAuthorizer against the real
// resource-centre tables, scoped to one Bundle's owner — spec-10 §2:
// "capabilities.tools 里的每个 ref 都要先经 Resource Registry 校验：存在、
// 未禁用、当前用户有权使用".
//
// A missing or disabled ref is simply not authorized (ok=false) rather than
// an error: the run has already passed its pre-flight check, so this is the
// last line of defence and it should decline quietly, not crash the graph.
type resourceAuthorizer struct {
	ctx     context.Context
	q       store.Querier
	ownerID int64
	aesKey  []byte
}

func newResourceAuthorizer(ctx context.Context, q store.Querier, ownerID int64, aesKey []byte) adk.ResourceAuthorizer {
	return &resourceAuthorizer{ctx: ctx, q: q, ownerID: ownerID, aesKey: aesKey}
}

// lookup is one kind's "newest row for this ref" query, plus the ADK kind a
// hit should surface as.
type lookup struct {
	kind  adk.ResourceKind
	query func() (config []byte, status int16, err error)
}

func (a *resourceAuthorizer) Authorize(_ context.Context, ref string) (adk.ToolSpec, bool, error) {
	// A ref may name a tool, an MCP server or a knowledge base (all three
	// surface via capabilities.tools[], per spec-05), or a skill. Knowledge
	// bases have no ADK kind of their own — they are endpoint-backed
	// lookups, so they surface as a plain tool call.
	for _, l := range []lookup{
		{adk.KindTool, func() ([]byte, int16, error) {
			row, err := a.q.GetToolLatestByRef(a.ctx, store.GetToolLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.Config, row.Status, err
		}},
		{adk.KindMCP, func() ([]byte, int16, error) {
			row, err := a.q.GetMCPServerLatestByRef(a.ctx, store.GetMCPServerLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.Config, row.Status, err
		}},
		{adk.KindTool, func() ([]byte, int16, error) {
			row, err := a.q.GetKnowledgeBaseLatestByRef(a.ctx, store.GetKnowledgeBaseLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.Config, row.Status, err
		}},
		{adk.KindSkill, func() ([]byte, int16, error) {
			row, err := a.q.GetSkillLatestByRef(a.ctx, store.GetSkillLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.Config, row.Status, err
		}},
	} {
		rawConfig, status, err := l.query()
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return adk.ToolSpec{}, false, err
		}
		if status != int16(resource.StatusEnabled) {
			return adk.ToolSpec{}, false, nil
		}
		config, err := a.decryptConfig(rawConfig)
		if err != nil {
			return adk.ToolSpec{}, false, err
		}
		return adk.ToolSpec{Ref: ref, Kind: l.kind, Config: config}, true, nil
	}
	return adk.ToolSpec{}, false, nil
}

// decryptConfig is the one place a stored credential is turned back into
// plaintext: a real tool needs the real key. The result goes only into the
// compiled tool, never into a response.
func (a *resourceAuthorizer) decryptConfig(raw []byte) (map[string]any, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	decrypted, err := resource.DecryptConfig(adaptercrypto.NewCipher(a.aesKey), resource.Config(config))
	if err != nil {
		return nil, err
	}
	return map[string]any(decrypted), nil
}
