package depclosure

import (
	"strings"
	"testing"
)

// fakeResolver is a plain in-memory graph: nodes map to their existence,
// publication status, and outgoing dependency edges. Lets the traversal be
// tested independent of Postgres, including graph shapes (cycles) today's
// real DSL schema can't actually produce.
type fakeResolver struct {
	published map[NodeKey]bool
	deps      map[NodeKey][]DependencyRef
	missing   map[NodeKey]bool // explicitly does-not-exist
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{
		published: map[NodeKey]bool{},
		deps:      map[NodeKey][]DependencyRef{},
		missing:   map[NodeKey]bool{},
	}
}

func (f *fakeResolver) Exists(node NodeKey) (bool, error) {
	return !f.missing[node], nil
}

func (f *fakeResolver) IsPublished(node NodeKey) (bool, error) {
	return f.published[node], nil
}

func (f *fakeResolver) Dependencies(node NodeKey) ([]DependencyRef, error) {
	return f.deps[node], nil
}

func (f *fakeResolver) setDeps(node NodeKey, deps ...DependencyRef) {
	f.deps[node] = deps
}

func TestValidate_AllDependenciesPublished_NoIssues(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	architect := NodeKey{Kind: "agent", Ref: "architect", Version: "1.0"}
	mcp := NodeKey{Kind: "mcp", Ref: "internal-search"}

	r.setDeps(bundle, DependencyRef{Node: architect, Field: "agents[0]"})
	r.setDeps(architect, DependencyRef{Node: mcp, Field: "agents[0].capabilities.tools[0]"})
	r.published[architect] = true
	r.published[mcp] = true

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestValidate_DirectUnpublishedDependency_OneIssue(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	architect := NodeKey{Kind: "agent", Ref: "architect", Version: "1.0"}
	r.setDeps(bundle, DependencyRef{Node: architect, Field: "agents[0]"})
	// architect left unpublished

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %+v", issues)
	}
	if !strings.Contains(issues[0].Reason, "未发布") {
		t.Fatalf("expected an '未发布' reason, got %q", issues[0].Reason)
	}
}

// TestValidate_TransitiveUnpublishedDependency_IsDetected is spec-08's
// explicit acceptance criterion: Bundle -> Agent published, but
// Agent -> MCP unpublished must still be caught — not just the first level.
func TestValidate_TransitiveUnpublishedDependency_IsDetected(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	architect := NodeKey{Kind: "agent", Ref: "architect", Version: "1.0"}
	mcp := NodeKey{Kind: "mcp", Ref: "internal-search"}

	r.setDeps(bundle, DependencyRef{Node: architect, Field: "agents[0]"})
	r.setDeps(architect, DependencyRef{Node: mcp, Field: "agents[0].capabilities.tools[2]"})
	r.published[architect] = true // published...
	// ...but its own dependency mcp is not

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue (the transitive one), got %+v", issues)
	}
	if !strings.Contains(issues[0].Reason, "internal-search") {
		t.Fatalf("expected the mcp dependency to be named in the reason, got %q", issues[0].Reason)
	}
	if !strings.Contains(issues[0].Reason, "architect@1.0") {
		t.Fatalf("expected the full path (through architect@1.0) in the reason, got %q", issues[0].Reason)
	}
}

// TestValidate_VersionMismatch_UnpublishedVersionIsDetected covers spec-08's
// "版本精确匹配" requirement: Agent v1.0 is published, but the Bundle
// depends on the (unpublished) v1.1 — must not be satisfied by v1.0 merely
// sharing the ref.
func TestValidate_VersionMismatch_UnpublishedVersionIsDetected(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	architectV11 := NodeKey{Kind: "agent", Ref: "architect", Version: "1.1"}
	architectV10 := NodeKey{Kind: "agent", Ref: "architect", Version: "1.0"}

	r.setDeps(bundle, DependencyRef{Node: architectV11, Field: "agents[0]"})
	r.published[architectV10] = true // wrong version published
	// architectV11 (the one actually referenced) is not published

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %+v", issues)
	}
	if !strings.Contains(issues[0].Reason, "architect@1.1") {
		t.Fatalf("expected the unpublished version 1.1 to be named, got %q", issues[0].Reason)
	}
}

// TestValidate_MultipleUnpublishedDependencies_AllReturnedAtOnce covers
// spec-08's "一次性全部返回，不是发现一个就中断".
func TestValidate_MultipleUnpublishedDependencies_AllReturnedAtOnce(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	pm := NodeKey{Kind: "agent", Ref: "product_manager", Version: "1.2"}
	architect := NodeKey{Kind: "agent", Ref: "architect", Version: "1.0"}
	mcp := NodeKey{Kind: "mcp", Ref: "internal-search"}

	r.setDeps(bundle,
		DependencyRef{Node: pm, Field: "agents[0]"},
		DependencyRef{Node: architect, Field: "agents[1]"},
	)
	r.setDeps(architect, DependencyRef{Node: mcp, Field: "agents[1].capabilities.tools[2]"})
	// pm unpublished, architect published but mcp (its dep) unpublished

	r.published[architect] = true

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected exactly 2 issues (pm and mcp), got %+v", issues)
	}
}

// TestValidate_CycleIsDetectedNotLooped is the other explicit acceptance
// criterion: a cycle must be reported once, not walked until timeout. This
// graph shape (Agent's hook referencing a Bundle that contains the same
// Agent) isn't something today's real DSL schema can construct, but the
// traversal must still terminate correctly if it ever does — this is the
// only way to exercise that path deterministically.
func TestValidate_CycleIsDetectedNotLooped(t *testing.T) {
	r := newFakeResolver()
	agentA := NodeKey{Kind: "agent", Ref: "a", Version: "1.0"}
	bundleB := NodeKey{Kind: "bundle", Ref: "b", Version: "1.0"}

	r.setDeps(agentA, DependencyRef{Node: bundleB, Field: "hooks[0]"})
	r.setDeps(bundleB, DependencyRef{Node: agentA, Field: "agents[0]"})
	r.published[bundleB] = true

	issues, err := Validate(agentA, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	cycles := 0
	for _, iss := range issues {
		if iss.Kind == ErrCycle {
			cycles++
		}
	}
	if cycles != 1 {
		t.Fatalf("expected exactly 1 cycle issue, got %d in %+v", cycles, issues)
	}
}

func TestValidate_MissingDependency_ReportsNotExists(t *testing.T) {
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "web-app-builder", Version: "2.0"}
	ghost := NodeKey{Kind: "agent", Ref: "ghost", Version: "9.9"}
	r.setDeps(bundle, DependencyRef{Node: ghost, Field: "agents[0]"})
	r.missing[ghost] = true

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Reason, "不存在") {
		t.Fatalf("expected a not-exists issue, got %+v", issues)
	}
}

func TestValidate_DiamondDependency_NotFalsePositiveCycle(t *testing.T) {
	// bundle -> agentA, agentB; both agentA and agentB -> shared mcp.
	// Visiting the shared leaf twice via different branches is NOT a cycle.
	r := newFakeResolver()
	bundle := NodeKey{Kind: "bundle", Ref: "multi-agent", Version: "1.0"}
	agentA := NodeKey{Kind: "agent", Ref: "a", Version: "1.0"}
	agentB := NodeKey{Kind: "agent", Ref: "b", Version: "1.0"}
	sharedMCP := NodeKey{Kind: "mcp", Ref: "shared"}

	r.setDeps(bundle,
		DependencyRef{Node: agentA, Field: "agents[0]"},
		DependencyRef{Node: agentB, Field: "agents[1]"},
	)
	r.setDeps(agentA, DependencyRef{Node: sharedMCP, Field: "agents[0].capabilities.tools[0]"})
	r.setDeps(agentB, DependencyRef{Node: sharedMCP, Field: "agents[1].capabilities.tools[0]"})
	r.published[agentA] = true
	r.published[agentB] = true
	r.published[sharedMCP] = true

	issues, err := Validate(bundle, r)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("a shared dependency reached via two branches must not be a false-positive cycle: %+v", issues)
	}
}
