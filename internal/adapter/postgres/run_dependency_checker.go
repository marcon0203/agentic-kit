package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RunDependencyChecker implements run.DependencyChecker: the pre-flight
// recheck that everything a Bundle transitively needs is still usable.
//
// It reports only the *kind* of unavailability, never which resource — the
// caller may be a subscriber who is not allowed to know what the Bundle is
// built from (spec-11 "错误信息必须脱敏").
type RunDependencyChecker struct {
	q       store.Querier
	catalog *ResourceCatalog
	keys    *ProviderKeyStore
}

func NewRunDependencyChecker(q store.Querier, catalog *ResourceCatalog, keys *ProviderKeyStore) *RunDependencyChecker {
	return &RunDependencyChecker{q: q, catalog: catalog, keys: keys}
}

var _ run.DependencyChecker = (*RunDependencyChecker)(nil)

func (c *RunDependencyChecker) Check(ctx context.Context, ownerID int64, bundleDef map[string]any) (run.DependencyStatus, error) {
	apiKeys, err := c.keys.Keys(ctx, ownerID)
	if err != nil {
		return run.DependenciesOK, err
	}

	agentsRaw, _ := bundleDef["agents"].([]any)
	for _, a := range agentsRaw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := am["ref"].(string)
		version, _ := am["version"].(string)

		row, err := ResolveAgentVersion(ctx, c.q, ownerID, ref, version)
		if errors.Is(err, pgx.ErrNoRows) {
			return run.DependencyAgentMissing, nil
		}
		if err != nil {
			return run.DependenciesOK, err
		}

		var def map[string]any
		if err := json.Unmarshal(row.Definition, &def); err != nil {
			return run.DependenciesOK, err
		}

		status, err := c.checkCapabilities(ctx, ownerID, def)
		if err != nil || status != run.DependenciesOK {
			return status, err
		}
		if status := checkProvider(def, apiKeys); status != run.DependenciesOK {
			return status, nil
		}
	}
	return run.DependenciesOK, nil
}

// checkCapabilities asks the same catalog the Agent context uses when it
// validates a new version — so "usable at publish time" and "usable at run
// time" cannot drift apart into two different definitions.
func (c *RunDependencyChecker) checkCapabilities(ctx context.Context, ownerID int64, agentDef map[string]any) (run.DependencyStatus, error) {
	caps, _ := agentDef["capabilities"].(map[string]any)

	for _, ref := range stringSlice(caps["tools"]) {
		st, err := c.catalog.ToolStatus(ctx, ownerID, ref)
		if err != nil {
			return run.DependenciesOK, err
		}
		if !usable(st) {
			return run.DependencyResourceUnavailable, nil
		}
	}
	for _, ref := range stringSlice(caps["skills"]) {
		st, err := c.catalog.SkillStatus(ctx, ownerID, ref)
		if err != nil {
			return run.DependenciesOK, err
		}
		if !usable(st) {
			return run.DependencyResourceUnavailable, nil
		}
	}
	return run.DependenciesOK, nil
}

func usable(st agent.RefStatus) bool { return st.Found && st.Enabled }

// checkProvider accepts a Bundle whose primary provider is unconfigured as
// long as one of the Agent's declared fallbacks is: the run would succeed,
// so refusing to start it would be wrong.
func checkProvider(agentDef map[string]any, apiKeys map[string]string) run.DependencyStatus {
	model, _ := agentDef["model"].(map[string]any)
	provider, _ := model["provider"].(string)
	if provider == "" {
		return run.DependenciesOK
	}
	if _, ok := apiKeys[provider]; ok {
		return run.DependenciesOK
	}
	for _, f := range stringSlice(model["fallback"]) {
		spec, err := modelgateway.ParseModelSpec(f)
		if err != nil {
			continue
		}
		if _, ok := apiKeys[spec.Provider]; ok {
			return run.DependenciesOK
		}
	}
	return run.DependencyProviderMissing
}

// ResolveAgentVersion reads one Agent version, or the newest one when no
// version is pinned. Exported because the compiler needs the exact same
// resolution the dependency check just performed.
func ResolveAgentVersion(ctx context.Context, q store.Querier, ownerID int64, ref, version string) (store.Agent, error) {
	if version != "" {
		return q.GetAgentForOwner(ctx, store.GetAgentForOwnerParams{OwnerUserID: ownerID, AgentRef: ref, Version: version})
	}
	return q.GetAgentLatestByRef(ctx, store.GetAgentLatestByRefParams{OwnerUserID: ownerID, AgentRef: ref})
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
