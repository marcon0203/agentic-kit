package adk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallEndpoint_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := callEndpoint(context.Background(), srv.Client(), srv.URL, "internal-search", `{"q":"go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCallEndpoint_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	_, err := callEndpoint(context.Background(), srv.Client(), srv.URL, "internal-search", "")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected an http 500 error, got %v", err)
	}
}

func TestCallEndpoint_NoEndpointConfigured(t *testing.T) {
	_, err := callEndpoint(context.Background(), http.DefaultClient, "", "internal-search", "")
	if err == nil {
		t.Fatal("expected an error for a resource with no endpoint")
	}
}

func TestSkillInstructions_PrefersInstructionsOverDescription(t *testing.T) {
	spec := ToolSpec{Config: map[string]any{"instructions": "do X carefully", "description": "does X"}}
	if got := skillInstructions(spec); got != "do X carefully" {
		t.Fatalf("expected instructions to win, got %q", got)
	}
}

func TestSkillInstructions_FallsBackToDescription(t *testing.T) {
	spec := ToolSpec{Config: map[string]any{"description": "does X"}}
	if got := skillInstructions(spec); got != "does X" {
		t.Fatalf("expected description fallback, got %q", got)
	}
}

func TestBuildTool_Skill(t *testing.T) {
	tl, err := BuildTool(ToolSpec{Ref: "code-review", Kind: KindSkill, Config: map[string]any{"instructions": "review carefully"}}, nil, nil)
	if err != nil {
		t.Fatalf("BuildTool: %v", err)
	}
	if tl.Name() != "code-review" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
	}
}

type fakeSkillContentFetcher struct {
	content string
	err     error
	calls   int
}

func (f *fakeSkillContentFetcher) Fetch(_ context.Context, _, _ int64, _ string) (string, error) {
	f.calls++
	return f.content, f.err
}

func TestBuildTool_OSSBackedSkill_BuildsWithFetcher(t *testing.T) {
	fetcher := &fakeSkillContentFetcher{content: "# SKILL.md body"}
	tl, err := BuildTool(ToolSpec{
		Ref: "zip-skill", Kind: KindSkill, OwnerID: 1, ResourceID: 7,
		Config: map[string]any{"oss_prefix": "skills/1/zip-skill/1.0", "instructions": "stale fallback"},
	}, nil, fetcher)
	if err != nil {
		t.Fatalf("BuildTool: %v", err)
	}
	if tl.Name() != "zip-skill" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
	}
}

// A "mcp" resource builds a tool.Toolset via BuildMCPToolset, not a single
// Tool via BuildTool — BuildTool rejects it outright so a caller can't
// silently get a broken half-built tool for the wrong resource kind.
func TestBuildTool_MCPIsRejected(t *testing.T) {
	_, err := BuildTool(ToolSpec{Ref: "internal-search", Kind: KindMCP, Config: map[string]any{"endpoint": "http://example.com"}}, nil, nil)
	if err == nil {
		t.Fatal("expected BuildTool to reject a KindMCP spec")
	}
}

func TestBuildMCPToolset_RequiresEndpoint(t *testing.T) {
	_, err := BuildMCPToolset(ToolSpec{Ref: "internal-search", Kind: KindMCP, Config: map[string]any{}})
	if err == nil {
		t.Fatal("expected an error for a resource with no endpoint")
	}
}

// Connecting to the MCP server happens lazily on first use (the SDK's own
// doc comment), so building the toolset against an unreachable endpoint
// should still succeed — only calling Tools()/a tool would fail.
func TestBuildMCPToolset_ConnectsLazily(t *testing.T) {
	ts, err := BuildMCPToolset(ToolSpec{Ref: "internal-search", Kind: KindMCP, Config: map[string]any{"endpoint": "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("BuildMCPToolset: %v", err)
	}
	if ts.Name() == "" {
		t.Fatal("expected a non-empty toolset name")
	}
}
