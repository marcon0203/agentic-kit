package adk

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

type fakeAuthorizer struct {
	authorized map[string]ToolSpec
}

func (f *fakeAuthorizer) Authorize(_ context.Context, ref string) (ToolSpec, bool, error) {
	if ref == "boom" {
		return ToolSpec{}, false, errors.New("registry unreachable")
	}
	spec, ok := f.authorized[ref]
	return spec, ok, nil
}

func TestCompileAgent_Success(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "architect", "role": "System Architect",
		"model":   map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona": "You are a system architect.",
		"capabilities": map[string]any{
			"tools":  []any{"internal-search"},
			"skills": []any{"code-review"},
		},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"internal-search": {Ref: "internal-search", Kind: KindMCP, Config: map[string]any{"endpoint": "http://example.com"}},
		"code-review":     {Ref: "code-review", Kind: KindSkill, Config: map[string]any{"instructions": "review the code"}},
	}}

	a, err := CompileAgent(context.Background(), def, AgentCompileOptions{
		Gateway: gw, APIKeys: map[string]string{"anthropic": "sk-test"}, Authorizer: authorizer,
	})
	if err != nil {
		t.Fatalf("CompileAgent: %v", err)
	}
	if a.Name() != "architect" {
		t.Fatalf("expected agent name architect, got %q", a.Name())
	}
}

func TestCompileAgent_UnauthorizedResource_ExcludedFromTools(t *testing.T) {
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model":        map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona":      "p",
		"capabilities": map[string]any{"tools": []any{"private-tool"}, "skills": []any{}},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{}} // private-tool not authorized

	tools, err := compileTools(context.Background(), def, authorizer)
	if err != nil {
		t.Fatalf("compileTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected an unauthorized ref to be silently omitted, got %d tools", len(tools))
	}
}

func TestCompileAgent_AuthorizerError_PropagatesAsCompileError(t *testing.T) {
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model":        map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona":      "p",
		"capabilities": map[string]any{"tools": []any{"boom"}, "skills": []any{}},
	}
	_, err := compileTools(context.Background(), def, &fakeAuthorizer{authorized: map[string]ToolSpec{}})
	if err == nil {
		t.Fatal("expected a registry error to fail compilation, not be silently swallowed")
	}
}

func TestCompileAgent_NoAPIKeyForProvider_ReturnsError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model":   map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona": "p",
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, APIKeys: map[string]string{}})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestCompileAgent_APIKeyOnlyForFallback_Succeeds(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model": map[string]any{
			"provider": "anthropic", "name": "claude-sonnet-5",
			"fallback": []any{"openai/gpt-4o"},
		},
		"persona": "p",
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, APIKeys: map[string]string{"openai": "sk-test"}})
	if err != nil {
		t.Fatalf("expected compilation to succeed when only a fallback provider has a key, got %v", err)
	}
}

func TestCompileAgent_MissingRef_ReturnsError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	_, err := CompileAgent(context.Background(), map[string]any{}, AgentCompileOptions{Gateway: gw})
	if err == nil {
		t.Fatal("expected an error for a definition missing the agent ref")
	}
}

func TestCompileAgent_InvalidFallbackSpec_ReturnsError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "a", "role": "r", "persona": "p",
		"model": map[string]any{"provider": "anthropic", "name": "claude-sonnet-5", "fallback": []any{"not-a-valid-spec"}},
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, APIKeys: map[string]string{"anthropic": "k"}})
	if err == nil {
		t.Fatal("expected an error for a malformed model.fallback entry")
	}
}
