package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// checkHandoffConsistency implements spec-07 point 5: an Agent's own
// handoff.accepts_input_from/produces_output_to declarations may not match
// the Bundle's actual edges — the two DSLs can be maintained by different
// people and drift out of sync. This is warning-only (spec-07: "两份 DSL
// 可能由不同角色维护，允许暂时脱节但要可见"), never blocking.
func checkHandoffConsistency(ctx context.Context, q store.Querier, ownerID int64, def map[string]any, g bundlegraph.Graph) ([]bundlegraph.Issue, error) {
	nodeToAgentRef := map[string]string{}
	agentsRaw, _ := def["agents"].([]any)
	for _, a := range agentsRaw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := am["ref"].(string)
		node, _ := am["alias"].(string)
		if node == "" {
			node = ref
		}
		nodeToAgentRef[node] = ref
	}

	handoffs := map[string]agentHandoff{}
	for _, agentRef := range nodeToAgentRef {
		if _, done := handoffs[agentRef]; done {
			continue
		}
		h, err := fetchAgentHandoff(ctx, q, ownerID, agentRef)
		if err != nil {
			return nil, err
		}
		handoffs[agentRef] = h
	}

	var warnings []bundlegraph.Issue
	for _, e := range g.Edges {
		fromRef := nodeToAgentRef[e.From]
		for _, to := range e.To {
			if to == bundlegraph.EndNode {
				continue
			}
			toRef := nodeToAgentRef[to]

			if h, ok := handoffs[toRef]; ok && len(h.AcceptsInputFrom) > 0 && !containsStr(h.AcceptsInputFrom, fromRef) {
				warnings = append(warnings, bundlegraph.Issue{
					Field:   fmt.Sprintf("orchestration.edges[%d]", e.Index),
					Message: fmt.Sprintf("Agent %q 的 handoff.accepts_input_from 未声明接受来自 %q 的输入，与实际编排边不一致", toRef, fromRef),
				})
			}
			if h, ok := handoffs[fromRef]; ok && len(h.ProducesOutputTo) > 0 && !containsStr(h.ProducesOutputTo, toRef) {
				warnings = append(warnings, bundlegraph.Issue{
					Field:   fmt.Sprintf("orchestration.edges[%d]", e.Index),
					Message: fmt.Sprintf("Agent %q 的 handoff.produces_output_to 未声明输出给 %q，与实际编排边不一致", fromRef, toRef),
				})
			}
		}
	}
	return warnings, nil
}

type agentHandoff struct {
	AcceptsInputFrom []string
	ProducesOutputTo []string
}

func fetchAgentHandoff(ctx context.Context, q store.Querier, ownerID int64, agentRef string) (agentHandoff, error) {
	if agentRef == "" {
		return agentHandoff{}, nil
	}
	row, err := q.GetAgentLatestByRef(ctx, store.GetAgentLatestByRefParams{OwnerUserID: ownerID, AgentRef: agentRef})
	if errors.Is(err, pgx.ErrNoRows) {
		return agentHandoff{}, nil // unknown agent ref: handled elsewhere, not this check's job
	}
	if err != nil {
		return agentHandoff{}, err
	}

	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return agentHandoff{}, err
	}
	handoffRaw, _ := def["handoff"].(map[string]any)
	return agentHandoff{
		AcceptsInputFrom: toStringSlice(handoffRaw["accepts_input_from"]),
		ProducesOutputTo: toStringSlice(handoffRaw["produces_output_to"]),
	}, nil
}

func containsStr(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
