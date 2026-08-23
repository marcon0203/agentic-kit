package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/depclosure"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// Dependency-graph node kinds, matching ListingResourceType in
// api/openapi.yaml.
const (
	DepKindAgent  = "agent"
	DepKindBundle = "bundle"
	DepKindSkill  = "skill"
	DepKindMCP    = "mcp"
)

// storeResolver implements depclosure.Resolver against the real database,
// scoped to one owner (spec-08: dependency closure never crosses ownership
// — a Bundle/Agent only ever references resources in the same owner's
// space by ref).
type storeResolver struct {
	ctx     context.Context
	q       store.Querier
	ownerID int64
}

func (r *storeResolver) Exists(node depclosure.NodeKey) (bool, error) {
	switch node.Kind {
	case DepKindAgent:
		_, err := r.q.GetAgentForOwner(r.ctx, store.GetAgentForOwnerParams{OwnerUserID: r.ownerID, AgentRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case DepKindBundle:
		_, err := r.q.GetBundleForOwner(r.ctx, store.GetBundleForOwnerParams{OwnerUserID: r.ownerID, BundleRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case DepKindSkill:
		_, err := r.q.GetSkillLatestStatusByRef(r.ctx, store.GetSkillLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	case DepKindMCP:
		_, err := r.q.GetMCPServerLatestStatusByRef(r.ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	default:
		return false, fmt.Errorf("depclosure: unknown kind %q", node.Kind)
	}
}

func (r *storeResolver) IsPublished(node depclosure.NodeKey) (bool, error) {
	switch node.Kind {
	case DepKindAgent:
		_, err := r.q.GetAgentListingForOwnerByRefVersion(r.ctx, store.GetAgentListingForOwnerByRefVersionParams{OwnerUserID: r.ownerID, AgentRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case DepKindBundle:
		_, err := r.q.GetBundleListingForOwnerByRefVersion(r.ctx, store.GetBundleListingForOwnerByRefVersionParams{OwnerUserID: r.ownerID, BundleRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case DepKindSkill:
		_, err := r.q.GetSkillListingForOwnerByRef(r.ctx, store.GetSkillListingForOwnerByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	case DepKindMCP:
		_, err := r.q.GetMCPServerListingForOwnerByRef(r.ctx, store.GetMCPServerListingForOwnerByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	default:
		return false, fmt.Errorf("depclosure: unknown kind %q", node.Kind)
	}
}

func (r *storeResolver) Dependencies(node depclosure.NodeKey) ([]depclosure.DependencyRef, error) {
	switch node.Kind {
	case DepKindBundle:
		return r.bundleDependencies(node)
	case DepKindAgent:
		return r.agentDependencies(node)
	default:
		// Skill and MCP are leaves: no further dependencies.
		return nil, nil
	}
}

func (r *storeResolver) bundleDependencies(node depclosure.NodeKey) ([]depclosure.DependencyRef, error) {
	row, err := r.q.GetBundleForOwner(r.ctx, store.GetBundleForOwnerParams{OwnerUserID: r.ownerID, BundleRef: node.Ref, Version: node.Version})
	if err != nil {
		return nil, err
	}
	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return nil, err
	}

	var deps []depclosure.DependencyRef
	agentsRaw, _ := def["agents"].([]any)
	for i, a := range agentsRaw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := am["ref"].(string)
		version, _ := am["version"].(string)
		if version == "" {
			// Bundle.agents[].version isn't required by the schema; fall
			// back to the owner's current latest version of that ref.
			latest, err := r.q.GetAgentLatestByRef(r.ctx, store.GetAgentLatestByRefParams{OwnerUserID: r.ownerID, AgentRef: ref})
			if err == nil {
				version = latest.Version
			}
		}
		deps = append(deps, depclosure.DependencyRef{
			Node:  depclosure.NodeKey{Kind: DepKindAgent, Ref: ref, Version: version},
			Field: fmt.Sprintf("agents[%d]", i),
		})
	}
	return deps, nil
}

func (r *storeResolver) agentDependencies(node depclosure.NodeKey) ([]depclosure.DependencyRef, error) {
	row, err := r.q.GetAgentForOwner(r.ctx, store.GetAgentForOwnerParams{OwnerUserID: r.ownerID, AgentRef: node.Ref, Version: node.Version})
	if err != nil {
		return nil, err
	}
	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return nil, err
	}

	tools := toStringSlice(deepGet(def, "capabilities", "tools"))
	skills := toStringSlice(deepGet(def, "capabilities", "skills"))

	var deps []depclosure.DependencyRef
	for i, ref := range tools {
		// Only mcp_servers-backed tool refs are publishable resources in
		// the closure graph — a plain `tools`/`knowledge_bases` ref has no
		// publish concept (ListingResourceType doesn't include either), so
		// it's auto-satisfied and simply not added as a dependency edge.
		if _, err := r.q.GetMCPServerLatestStatusByRef(r.ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: ref}); err == nil {
			deps = append(deps, depclosure.DependencyRef{
				Node:  depclosure.NodeKey{Kind: DepKindMCP, Ref: ref},
				Field: fmt.Sprintf("capabilities.tools[%d]", i),
			})
		}
	}
	for i, ref := range skills {
		deps = append(deps, depclosure.DependencyRef{
			Node:  depclosure.NodeKey{Kind: DepKindSkill, Ref: ref},
			Field: fmt.Sprintf("capabilities.skills[%d]", i),
		})
	}
	return deps, nil
}

func existsFromErr(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
