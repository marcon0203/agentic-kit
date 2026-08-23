// Package bundlegraph implements the Bundle orchestration graph's static
// validation (spec-07-bundle-center): entry existence, edge-target
// declaration, self-loop-aware deadlock detection, reachability warnings,
// and edge.condition expression syntax checking. It assumes the input has
// already passed JSON-Schema validation (internal/dslschema) — this
// package checks graph *semantics*, not document shape.
package bundlegraph

import (
	"fmt"

	"github.com/expr-lang/expr"
)

// EndNode is the literal sink target meaning "flow ends here" — not a
// declared agent node, and never a validation error on its own.
const EndNode = "END"

// Edge mirrors BundleDefinition.orchestration.edges (api/openapi.yaml):
// To is already flattened from the DSL's string-or-array shape, and Index
// is this edge's position for field-path error reporting.
type Edge struct {
	Index     int
	From      string
	To        []string
	Condition string
	// Mode and Join are execution hints (internal/orchestrator/adk consumes
	// them) rather than static-validation inputs — this package's own
	// Validate never branches on them. Mode defaults to "sequential", Join
	// to "wait_all", per api/openapi.yaml's edge schema defaults.
	Mode string
	Join string
}

// Graph is the parsed orchestration block ready for validation.
type Graph struct {
	Nodes []string // agents[].alias if set, else agents[].ref
	Entry string
	Edges []Edge
}

// Issue is one validation finding — Field points at the offending DSL path
// (e.g. "orchestration.edges[2].to") for the frontend to highlight.
type Issue struct {
	Field   string
	Message string
}

// Result separates blocking errors (422 + 40003, per spec-07) from
// non-blocking warnings (unreachable nodes, handoff mismatches — spec-07
// points 4-5 — allowed to save so editing can proceed incrementally).
type Result struct {
	Errors   []Issue
	Warnings []Issue
}

func (r Result) Valid() bool { return len(r.Errors) == 0 }

// Validate runs every spec-07 graph rule against g.
func Validate(g Graph) Result {
	var res Result

	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n] = true
	}

	if g.Entry == "" || !nodeSet[g.Entry] {
		res.Errors = append(res.Errors, Issue{
			Field:   "orchestration.entry",
			Message: fmt.Sprintf("entry %q is not one of the declared agents", g.Entry),
		})
	}

	for _, e := range g.Edges {
		if !nodeSet[e.From] {
			res.Errors = append(res.Errors, Issue{
				Field:   fmt.Sprintf("orchestration.edges[%d].from", e.Index),
				Message: fmt.Sprintf("node %q is not declared in agents[]", e.From),
			})
		}
		for _, to := range e.To {
			if to != EndNode && !nodeSet[to] {
				res.Errors = append(res.Errors, Issue{
					Field:   fmt.Sprintf("orchestration.edges[%d].to", e.Index),
					Message: fmt.Sprintf("node %q is not declared in agents[]", to),
				})
			}
		}
		if e.Condition != "" {
			if _, err := expr.Compile(e.Condition, expr.Env(conditionEnv{})); err != nil {
				res.Errors = append(res.Errors, Issue{
					Field:   fmt.Sprintf("orchestration.edges[%d].condition", e.Index),
					Message: fmt.Sprintf("invalid expression: %v", err),
				})
			}
		}
	}

	// Deadlock / reachability only make sense once every edge endpoint is
	// a real node — skip if the checks above already found a bad
	// reference, so this doesn't pile on confusing secondary errors.
	if res.Valid() {
		res.append(checkReachability(g, nodeSet))
	}

	return res
}

// conditionEnv is the shape edge.condition expressions compile against —
// only shared_state is available to a routing condition (架构设计文档 6:
// conditions read shared_state, nothing else).
type conditionEnv struct {
	SharedState map[string]any `expr:"shared_state"`
}

func (r *Result) append(other Result) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// checkReachability implements spec-07 rules 3-4 together, since they're
// two outcomes of the same computation: BFS from entry *excluding
// self-loop edges* (a self-loop can't be how a node is first reached — you
// have to already be there to loop). A non-entry node the BFS never
// touches is unreachable; if that node also has a self-loop pointing back
// at itself, a naive indegree count would (wrongly) see that self-loop as
// "an incoming edge" and never flag the node as broken — exactly the POC
// deadlock bug — so that case is escalated from warning to error.
func checkReachability(g Graph, nodeSet map[string]bool) Result {
	var res Result

	adjacency := make(map[string][]string, len(nodeSet))
	selfLoop := make(map[string]bool, len(nodeSet))
	edgesByTarget := make(map[string][]int)

	for _, e := range g.Edges {
		for _, to := range e.To {
			if to == EndNode {
				continue
			}
			edgesByTarget[to] = append(edgesByTarget[to], e.Index)
			if e.From == to {
				selfLoop[to] = true
				continue
			}
			adjacency[e.From] = append(adjacency[e.From], to)
		}
	}

	reachable := map[string]bool{g.Entry: true}
	queue := []string{g.Entry}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[cur] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}

	for _, n := range g.Nodes {
		if n == g.Entry || reachable[n] {
			continue
		}
		if selfLoop[n] {
			res.Errors = append(res.Errors, Issue{
				Field:   fmt.Sprintf("orchestration.edges[%d]", firstOf(edgesByTarget[n])),
				Message: fmt.Sprintf("node %q can only be reached via its own retry edge and has no other incoming edge — it can never run for the first time (deadlock)", n),
			})
			continue
		}
		res.Warnings = append(res.Warnings, Issue{
			Field:   "orchestration.entry",
			Message: fmt.Sprintf("node %q is not reachable from entry %q", n, g.Entry),
		})
	}

	return res
}

func firstOf(indices []int) int {
	if len(indices) == 0 {
		return -1
	}
	return indices[0]
}
