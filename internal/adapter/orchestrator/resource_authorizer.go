package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

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
	ctx        context.Context
	q          store.Querier
	ownerID    int64
	aesKey     []byte
	pluginWasm *pluginWasmFetcher
	connectors *connectorRegistry

	boundMu   sync.Mutex
	boundRefs []string
}

// newResourceAuthorizer returns the concrete type (not just
// adk.ResourceAuthorizer) so callers that need to release what this
// authorizer bound during one run (spec-20 §4.5's "谁创建谁回收" for
// connector connections) can call ReleaseBound after the run ends.
// connectors may be nil — a deployment with no connector backend
// configured just never resolves a connector_resource_id.
func newResourceAuthorizer(ctx context.Context, q store.Querier, ownerID int64, aesKey []byte, pluginWasm *pluginWasmFetcher, connectors *connectorRegistry) *resourceAuthorizer {
	return &resourceAuthorizer{ctx: ctx, q: q, ownerID: ownerID, aesKey: aesKey, pluginWasm: pluginWasm, connectors: connectors}
}

// ReleaseBound releases every connector connection this authorizer bound
// while authorizing refs for one run. Safe to call once, at run end, even
// if nothing was ever bound.
func (a *resourceAuthorizer) ReleaseBound() {
	if a.connectors == nil {
		return
	}
	a.boundMu.Lock()
	refs := a.boundRefs
	a.boundRefs = nil
	a.boundMu.Unlock()
	for _, ref := range refs {
		a.connectors.Release(ref)
	}
}

// lookup is one kind's "newest row for this ref" query, plus the ADK kind a
// hit should surface as.
type lookup struct {
	kind  adk.ResourceKind
	query func() (id int64, config []byte, status int16, err error)
}

func (a *resourceAuthorizer) Authorize(_ context.Context, ref string) (adk.ToolSpec, bool, error) {
	// A "plugin:{id}/{name}" ref (spec-20 §5.1) resolves against
	// plugin_installations instead of the four resource-center tables —
	// same capabilities.tools[] array, a different backing store.
	if pluginID, name, ok := parsePluginRef(ref); ok {
		return a.authorizePlugin(pluginID, name)
	}

	// A ref may name a tool, an MCP server, a knowledge base or a skill —
	// all four surface via capabilities.tools[]/skills[], per spec-05.
	for _, l := range []lookup{
		{adk.KindTool, func() (int64, []byte, int16, error) {
			row, err := a.q.GetToolLatestByRef(a.ctx, store.GetToolLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.ID, row.Config, row.Status, err
		}},
		{adk.KindMCP, func() (int64, []byte, int16, error) {
			row, err := a.q.GetMCPServerLatestByRef(a.ctx, store.GetMCPServerLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.ID, row.Config, row.Status, err
		}},
		{adk.KindKnowledgeBase, func() (int64, []byte, int16, error) {
			row, err := a.q.GetKnowledgeBaseLatestByRef(a.ctx, store.GetKnowledgeBaseLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.ID, row.Config, row.Status, err
		}},
		{adk.KindSkill, func() (int64, []byte, int16, error) {
			row, err := a.q.GetSkillLatestByRef(a.ctx, store.GetSkillLatestByRefParams{OwnerUserID: a.ownerID, Ref: ref})
			return row.ID, row.Config, row.Status, err
		}},
	} {
		id, rawConfig, status, err := l.query()
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
		return adk.ToolSpec{Ref: ref, Kind: l.kind, Config: config, OwnerID: a.ownerID, ResourceID: id}, true, nil
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
