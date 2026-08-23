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
	tl, err := BuildTool(ToolSpec{Ref: "code-review", Kind: KindSkill, Config: map[string]any{"instructions": "review carefully"}})
	if err != nil {
		t.Fatalf("BuildTool: %v", err)
	}
	if tl.Name() != "code-review" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
	}
}

func TestBuildTool_MCP(t *testing.T) {
	tl, err := BuildTool(ToolSpec{Ref: "internal-search", Kind: KindMCP, Config: map[string]any{"endpoint": "http://example.com"}})
	if err != nil {
		t.Fatalf("BuildTool: %v", err)
	}
	if tl.Name() != "internal-search" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
	}
}
