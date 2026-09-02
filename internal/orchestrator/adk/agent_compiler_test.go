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
		"model":   map[string]any{"provider": "deepseek", "name": "deepseek-chat"},
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
		Gateway: gw, Credentials: map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}}, Authorizer: authorizer,
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
		"model":        map[string]any{"provider": "deepseek", "name": "deepseek-chat"},
		"persona":      "p",
		"capabilities": map[string]any{"tools": []any{"private-tool"}, "skills": []any{}},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{}} // private-tool not authorized

	tools, _, err := compileTools(context.Background(), def, authorizer, nil, nil, nil, nil)
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
		"model":        map[string]any{"provider": "deepseek", "name": "deepseek-chat"},
		"persona":      "p",
		"capabilities": map[string]any{"tools": []any{"boom"}, "skills": []any{}},
	}
	_, _, err := compileTools(context.Background(), def, &fakeAuthorizer{authorized: map[string]ToolSpec{}}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected a registry error to fail compilation, not be silently swallowed")
	}
}

func TestCompileAgent_NoAPIKeyForProvider_ReturnsError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model":   map[string]any{"provider": "deepseek", "name": "deepseek-chat"},
		"persona": "p",
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, Credentials: map[string]modelgateway.Credential{}})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestCompileAgent_APIKeyOnlyForFallback_Succeeds(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil)
	def := map[string]any{
		"agent": "architect", "role": "R",
		"model": map[string]any{
			"provider": "deepseek", "name": "deepseek-chat",
			"fallback": []any{"volcengine/doubao-seed-1-6"},
		},
		"persona": "p",
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, Credentials: map[string]modelgateway.Credential{"volcengine": {APIKey: "sk-test"}}})
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
		"model": map[string]any{"provider": "deepseek", "name": "deepseek-chat", "fallback": []any{"not-a-valid-spec"}},
	}
	_, err := CompileAgent(context.Background(), def, AgentCompileOptions{Gateway: gw, Credentials: map[string]modelgateway.Credential{"deepseek": {APIKey: "k"}}})
	if err == nil {
		t.Fatal("expected an error for a malformed model.fallback entry")
	}
}

// TestAppendRendererInstructions is the regression test for "图表数据缺少
// labels 字段": an auto_render registration has no input_schema/description
// round-trip through the model's own function-calling API the way a tool
// call does, so if its manifest description never reaches the persona, the
// model is left guessing the fenced-block shape entirely on its own.
func TestAppendRendererInstructions_AutoRenderWithDescription_Appended(t *testing.T) {
	regs := []RendererRegistration{
		{RendererName: "chart", FencedLangs: []string{"chart"}, Description: "emit a ```chart block shaped {labels, datasets}"},
	}
	got := appendRendererInstructions("You are a helpful assistant.", regs)
	want := "You are a helpful assistant.\n\nemit a ```chart block shaped {labels, datasets}"
	if got != want {
		t.Fatalf("appendRendererInstructions = %q, want %q", got, want)
	}
}

func TestAppendRendererInstructions_SkipsExplicitToolUIRegistration(t *testing.T) {
	// TriggerTool set, FencedLangs empty: an explicit tools[].ui
	// registration whose format is already covered by the tool's own
	// description/input_schema — appending here would just be noise.
	regs := []RendererRegistration{{RendererName: "chart", TriggerTool: "render_chart", Description: "should be ignored"}}
	got := appendRendererInstructions("persona", regs)
	if got != "persona" {
		t.Fatalf("expected persona unchanged, got %q", got)
	}
}

func TestAppendRendererInstructions_SkipsAutoRenderWithNoDescription(t *testing.T) {
	regs := []RendererRegistration{{RendererName: "chart", FencedLangs: []string{"chart"}}}
	got := appendRendererInstructions("persona", regs)
	if got != "persona" {
		t.Fatalf("expected persona unchanged for a renderer with no description, got %q", got)
	}
}

func TestAppendRendererInstructions_MultipleRenderers_JoinedWithBlankLine(t *testing.T) {
	regs := []RendererRegistration{
		{RendererName: "chart", FencedLangs: []string{"chart"}, Description: "chart format"},
		{RendererName: "table", FencedLangs: []string{"table"}, Description: "table format"},
	}
	got := appendRendererInstructions("persona", regs)
	want := "persona\n\nchart format\n\ntable format"
	if got != want {
		t.Fatalf("appendRendererInstructions = %q, want %q", got, want)
	}
}
