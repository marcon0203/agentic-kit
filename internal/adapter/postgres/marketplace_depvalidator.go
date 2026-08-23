package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/depclosure"
	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// DependencyValidator implements marketplace.DependencyValidator by walking
// the real database with internal/depclosure. The traversal algorithm is a
// pure package; this adapter is only the resolver that answers "does this
// node exist / is it published / what does it depend on".
type DependencyValidator struct{ q store.Querier }

func NewDependencyValidator(q store.Querier) *DependencyValidator {
	return &DependencyValidator{q: q}
}

var _ marketplace.DependencyValidator = (*DependencyValidator)(nil)

func (v *DependencyValidator) Validate(ctx context.Context, ownerID int64, kind marketplace.Kind, ref, version string) ([]marketplace.DependencyIssue, error) {
	root := depclosure.NodeKey{Kind: string(kind), Ref: ref, Version: version}
	issues, err := depclosure.Validate(root, &storeResolver{ctx: ctx, q: v.q, ownerID: ownerID})
	if err != nil {
		return nil, err
	}
	out := make([]marketplace.DependencyIssue, len(issues))
	for i, iss := range issues {
		out[i] = marketplace.DependencyIssue{
			Field: iss.Field, Reason: iss.Reason, Cycle: iss.Kind == depclosure.ErrCycle,
		}
	}
	return out, nil
}

// storeResolver answers depclosure's questions against Postgres, scoped to
// one owner: a dependency closure never crosses ownership (spec-08), since a
// Bundle/Agent only references resources in its own owner's space by ref.
type storeResolver struct {
	ctx     context.Context
	q       store.Querier
	ownerID int64
}

func (r *storeResolver) Exists(node depclosure.NodeKey) (bool, error) {
	switch marketplace.Kind(node.Kind) {
	case marketplace.KindAgent:
		_, err := r.q.GetAgentForOwner(r.ctx, store.GetAgentForOwnerParams{OwnerUserID: r.ownerID, AgentRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case marketplace.KindBundle:
		_, err := r.q.GetBundleForOwner(r.ctx, store.GetBundleForOwnerParams{OwnerUserID: r.ownerID, BundleRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case marketplace.KindSkill:
		_, err := r.q.GetSkillLatestStatusByRef(r.ctx, store.GetSkillLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	case marketplace.KindMCP:
		_, err := r.q.GetMCPServerLatestStatusByRef(r.ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	default:
		return false, fmt.Errorf("depclosure: unknown kind %q", node.Kind)
	}
}

func (r *storeResolver) IsPublished(node depclosure.NodeKey) (bool, error) {
	switch marketplace.Kind(node.Kind) {
	case marketplace.KindAgent:
		_, err := r.q.GetAgentListingForOwnerByRefVersion(r.ctx, store.GetAgentListingForOwnerByRefVersionParams{OwnerUserID: r.ownerID, AgentRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case marketplace.KindBundle:
		_, err := r.q.GetBundleListingForOwnerByRefVersion(r.ctx, store.GetBundleListingForOwnerByRefVersionParams{OwnerUserID: r.ownerID, BundleRef: node.Ref, Version: node.Version})
		return existsFromErr(err)
	case marketplace.KindSkill:
		_, err := r.q.GetSkillListingForOwnerByRef(r.ctx, store.GetSkillListingForOwnerByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	case marketplace.KindMCP:
		_, err := r.q.GetMCPServerListingForOwnerByRef(r.ctx, store.GetMCPServerListingForOwnerByRefParams{OwnerUserID: r.ownerID, Ref: node.Ref})
		return existsFromErr(err)
	default:
		return false, fmt.Errorf("depclosure: unknown kind %q", node.Kind)
	}
}

func (r *storeResolver) Dependencies(node depclosure.NodeKey) ([]depclosure.DependencyRef, error) {
	switch marketplace.Kind(node.Kind) {
	case marketplace.KindBundle:
		return r.bundleDependencies(node)
	case marketplace.KindAgent:
		return r.agentDependencies(node)
	default:
		// Skill and MCP are leaves.
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
			if latest, err := r.q.GetAgentLatestByRef(r.ctx, store.GetAgentLatestByRefParams{OwnerUserID: r.ownerID, AgentRef: ref}); err == nil {
				version = latest.Version
			}
		}
		deps = append(deps, depclosure.DependencyRef{
			Node:  depclosure.NodeKey{Kind: string(marketplace.KindAgent), Ref: ref, Version: version},
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

	tools := dslStringSlice(def, "capabilities", "tools")
	skills := dslStringSlice(def, "capabilities", "skills")

	var deps []depclosure.DependencyRef
	for i, ref := range tools {
		// Only mcp_servers-backed tool refs are publishable resources in the
		// closure graph — a plain tools/knowledge_bases ref has no publish
		// concept (ListingResourceType excludes both), so it is
		// auto-satisfied and simply not added as an edge.
		if _, err := r.q.GetMCPServerLatestStatusByRef(r.ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: r.ownerID, Ref: ref}); err == nil {
			deps = append(deps, depclosure.DependencyRef{
				Node:  depclosure.NodeKey{Kind: string(marketplace.KindMCP), Ref: ref},
				Field: fmt.Sprintf("capabilities.tools[%d]", i),
			})
		}
	}
	for i, ref := range skills {
		deps = append(deps, depclosure.DependencyRef{
			Node:  depclosure.NodeKey{Kind: string(marketplace.KindSkill), Ref: ref},
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

// dslStringSlice walks a nested DSL path and returns it as a string slice.
func dslStringSlice(m map[string]any, path ...string) []string {
	var cur any = m
	for _, key := range path {
		asMap, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = asMap[key]
	}
	arr, ok := cur.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
