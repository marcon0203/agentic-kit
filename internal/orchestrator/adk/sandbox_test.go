package adk

import (
	"context"
	"testing"
)

func TestBuildSandboxTools_ReturnsRunCodeAndExecuteCommand(t *testing.T) {
	spec := ToolSpec{Ref: "dev-sandbox", Config: map[string]any{
		"component_type": "sandbox", "api_url": "https://daytona.local", "api_key": "secret",
	}}

	tools, err := BuildSandboxTools(spec)
	if err != nil {
		t.Fatalf("BuildSandboxTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (run_code, execute_command), got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name()] = true
	}
	if !names["dev-sandbox_run_code"] || !names["dev-sandbox_execute_command"] {
		t.Fatalf("expected dev-sandbox_run_code and dev-sandbox_execute_command, got %v", names)
	}
}

func TestBuildSandboxTools_NoAPIURL_ReturnsError(t *testing.T) {
	spec := ToolSpec{Ref: "dev-sandbox", Config: map[string]any{"component_type": "sandbox"}}

	if _, err := BuildSandboxTools(spec); err == nil {
		t.Fatal("expected an error when api_url is missing")
	}
}

// compileTools must dispatch a component_type=sandbox resource to
// BuildSandboxTools (two tools), the same way it special-cases KindMCP into
// a Toolset instead of BuildTool's single-Tool path.
func TestCompileTools_SandboxComponent_ProducesTwoTools(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{"tools": []any{"dev-sandbox"}},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"dev-sandbox": {
			Ref: "dev-sandbox", Kind: KindTool,
			Config: map[string]any{"component_type": "sandbox", "api_url": "https://daytona.local", "api_key": "secret"},
		},
	}}

	tools, toolsets, err := compileTools(context.Background(), def, authorizer, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compileTools: %v", err)
	}
	if len(toolsets) != 0 {
		t.Fatalf("a sandbox component should not produce a Toolset, got %d", len(toolsets))
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools from the sandbox component, got %d", len(tools))
	}
}

// A plain "tool" kind resource (no component_type, or a non-sandbox one)
// must keep going through the ordinary BuildTool single-tool path.
func TestCompileTools_PlainToolComponent_UnaffectedBySandboxDispatch(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{"tools": []any{"http-tool"}},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"http-tool": {Ref: "http-tool", Kind: KindTool, Config: map[string]any{"endpoint": "http://example.com"}},
	}}

	tools, toolsets, err := compileTools(context.Background(), def, authorizer, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compileTools: %v", err)
	}
	if len(toolsets) != 0 || len(tools) != 1 {
		t.Fatalf("expected exactly 1 plain tool and no toolsets, got tools=%d toolsets=%d", len(tools), len(toolsets))
	}
}
