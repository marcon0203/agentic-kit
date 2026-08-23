package bundlegraph

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	dir, err := filepath.Abs("../../schemas/examples")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

// TestWebAppBuilderFixture_SelfLoopDoesNotFalsePositiveDeadlock is the
// regression test spec-07 explicitly calls for: the real fixture has
// fullstack_engineer -> fullstack_engineer as a retry edge, and
// fullstack_engineer is also reached externally (architect -> ui_designer/
// fullstack_engineer, then ui_designer -> fullstack_engineer). A validator
// that (wrongly) counts the self-loop toward the node's incoming-edge
// count would still pass this case by accident; the real bug was a node
// reachable *only* via its own self-loop — covered separately below.
func TestWebAppBuilderFixture_SelfLoopDoesNotFalsePositiveDeadlock(t *testing.T) {
	def := loadFixture(t, "web-app-builder.bundle.yaml")
	g, err := ParseGraph(def)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result := Validate(g)
	if !result.Valid() {
		t.Fatalf("expected the fixture to validate cleanly, got errors: %+v", result.Errors)
	}
	for _, w := range result.Warnings {
		if w.Field == "orchestration.entry" {
			t.Fatalf("fullstack_engineer's self-loop must not cause any unreachability warning: %+v", result.Warnings)
		}
	}
}

// TestSelfLoopOnlyIncomingEdge_IsDetectedAsDeadlock is the other direction
// of the regression: a node whose *only* incoming edge is its own retry
// edge is genuinely unreachable on first execution and must be a blocking
// error, not silently accepted.
func TestSelfLoopOnlyIncomingEdge_IsDetectedAsDeadlock(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "b"},
		Entry: "a",
		Edges: []Edge{
			{Index: 0, From: "a", To: []string{EndNode}},
			{Index: 1, From: "b", To: []string{"b"}}, // b's only incoming edge is from itself
		},
	}

	result := Validate(g)
	if result.Valid() {
		t.Fatal("expected a node reachable only via its own self-loop to be a validation error")
	}
	found := false
	for _, e := range result.Errors {
		if e.Field == "orchestration.edges[1]" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the deadlock error to point at edges[1], got: %+v", result.Errors)
	}
}

func TestValidate_MissingEntryIsError(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "b"},
		Entry: "not-declared",
		Edges: []Edge{{Index: 0, From: "a", To: []string{"b"}}},
	}
	result := Validate(g)
	if result.Valid() {
		t.Fatal("expected a missing entry to be an error")
	}
	if result.Errors[0].Field != "orchestration.entry" {
		t.Fatalf("expected error field orchestration.entry, got %q", result.Errors[0].Field)
	}
}

func TestValidate_EdgeToUndeclaredNodeIsError(t *testing.T) {
	g := Graph{
		Nodes: []string{"a"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "a", To: []string{"ghost"}}},
	}
	result := Validate(g)
	if result.Valid() {
		t.Fatal("expected an edge to an undeclared node to be an error")
	}
}

func TestValidate_EdgeFromUndeclaredNodeIsError(t *testing.T) {
	g := Graph{
		Nodes: []string{"a"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "ghost", To: []string{"a"}}},
	}
	result := Validate(g)
	if result.Valid() {
		t.Fatal("expected an edge from an undeclared node to be an error")
	}
}

func TestValidate_ToEndIsAlwaysValid(t *testing.T) {
	g := Graph{
		Nodes: []string{"a"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "a", To: []string{EndNode}}},
	}
	result := Validate(g)
	if !result.Valid() {
		t.Fatalf("expected to:END to be valid, got errors: %+v", result.Errors)
	}
}

func TestValidate_UnreachableNodeWithoutSelfLoopIsWarningNotError(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "orphan"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "a", To: []string{EndNode}}},
	}
	result := Validate(g)
	if !result.Valid() {
		t.Fatalf("an unreachable node with no self-loop must be a warning, not an error: %+v", result.Errors)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %+v", result.Warnings)
	}
}

func TestValidate_InvalidConditionExpressionIsError(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "b"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "a", To: []string{"b"}, Condition: "shared_state.tests_passed ==="}},
	}
	result := Validate(g)
	if result.Valid() {
		t.Fatal("expected a syntactically invalid condition expression to be rejected at save time")
	}
	if result.Errors[0].Field != "orchestration.edges[0].condition" {
		t.Fatalf("expected error on edges[0].condition, got %q", result.Errors[0].Field)
	}
}

func TestValidate_ValidConditionExpressionPasses(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "b"},
		Entry: "a",
		Edges: []Edge{{Index: 0, From: "a", To: []string{"b"}, Condition: "shared_state.tests_passed == true"}},
	}
	result := Validate(g)
	if !result.Valid() {
		t.Fatalf("expected a valid condition expression to pass, got errors: %+v", result.Errors)
	}
}

func TestValidate_FanOutParallelEdgeToMultipleTargets(t *testing.T) {
	g := Graph{
		Nodes: []string{"a", "b", "c"},
		Entry: "a",
		Edges: []Edge{
			{Index: 0, From: "a", To: []string{"b", "c"}},
			{Index: 1, From: "b", To: []string{EndNode}},
			{Index: 2, From: "c", To: []string{EndNode}},
		},
	}
	result := Validate(g)
	if !result.Valid() {
		t.Fatalf("expected a fan-out edge to multiple valid targets to pass, got errors: %+v", result.Errors)
	}
}
