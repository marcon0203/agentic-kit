package adk

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
)

// declaredWith mirrors toolinternal.RequestProcessor structurally — the
// interface BuildPluginTool's returned tool.Tool must satisfy for ADK's
// own flow to accept it at all (base_flow.go's toolPreprocess errors out
// for any tool.Tool that doesn't implement this). Declaring it locally
// avoids importing ADK's unexported internal package just for a test.
type requestProcessor interface {
	ProcessRequest(ctx agent.ToolContext, req *model.LLMRequest) error
}

type fakePluginRuntime struct {
	calls   int
	wasmKey string
	fn      string
	input   []byte
	output  []byte
	err     error
}

func (f *fakePluginRuntime) Call(_ context.Context, wasmKey string, _ []byte, _ PluginRuntimeOptions, funcName string, input []byte) ([]byte, error) {
	f.calls++
	f.wasmKey, f.fn, f.input = wasmKey, funcName, input
	return f.output, f.err
}

func TestBuildPluginTool_RequiresConfig(t *testing.T) {
	_, err := BuildPluginTool(ToolSpec{Ref: "acme.charts/render_chart", Kind: KindPlugin, Config: map[string]any{}}, &fakePluginRuntime{})
	if err == nil {
		t.Fatal("expected an error for a plugin spec missing wasm_key/wasm_bytes/func_name")
	}
}

func TestBuildPluginTool_RequiresRuntime(t *testing.T) {
	spec := ToolSpec{Ref: "acme.charts/render_chart", Kind: KindPlugin, Config: map[string]any{
		PluginConfigKeyWasmKey:   "acme.charts@1.0.0",
		PluginConfigKeyWasmBytes: []byte{0},
		PluginConfigKeyFuncName:  "render_chart",
	}}
	if _, err := BuildPluginTool(spec, nil); err == nil {
		t.Fatal("expected an error when no PluginRuntime is configured")
	}
}

func TestBuildPluginTool_BuildsSuccessfully(t *testing.T) {
	spec := ToolSpec{Ref: "acme.charts/render_chart", Kind: KindPlugin, Config: map[string]any{
		PluginConfigKeyWasmKey:      "acme.charts@1.0.0",
		PluginConfigKeyWasmBytes:    []byte{0x00, 0x61, 0x73, 0x6d},
		PluginConfigKeyFuncName:     "render_chart",
		PluginConfigKeyAllowedHosts: []string{"api.acme.example"},
	}}
	tl, err := BuildPluginTool(spec, &fakePluginRuntime{})
	if err != nil {
		t.Fatalf("BuildPluginTool: %v", err)
	}
	if tl.Name() != "acme_charts_render_chart" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
	}
}

// TestBuildPluginTool_DeclaresManifestInputSchema is the regression test
// for "agent 不知道参数怎么写": a plugin tool's manifest input_schema must
// actually reach the model's tool declaration, not just exist unused in
// spec.Config. This exercises the real pipeline end to end — ProcessRequest
// (which is exactly what ADK's own flow calls before every model
// round-trip) packs the declaration into req.Config.Tools, and
// declaredTools reads it back out — rather than asserting against
// BuildPluginTool's return value directly, which would miss a regression
// in either half of that handoff.
func TestBuildPluginTool_DeclaresManifestInputSchema(t *testing.T) {
	spec := ToolSpec{Ref: "acme.sql/run_query", Kind: KindPlugin, Config: map[string]any{
		PluginConfigKeyWasmKey:   "acme.sql@1.0.0",
		PluginConfigKeyWasmBytes: []byte{0x00, 0x61, 0x73, 0x6d},
		PluginConfigKeyFuncName:  "run_query",
		PluginConfigKeyInputSchema: map[string]any{
			"type":     "object",
			"required": []any{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "SQL to run"},
			},
		},
	}}
	tl, err := BuildPluginTool(spec, &fakePluginRuntime{})
	if err != nil {
		t.Fatalf("BuildPluginTool: %v", err)
	}
	rp, ok := tl.(requestProcessor)
	if !ok {
		t.Fatal("expected the built tool to implement ProcessRequest (ADK's flow requires it on every tool)")
	}
	req := &model.LLMRequest{}
	if err := rp.ProcessRequest(nil, req); err != nil {
		t.Fatalf("ProcessRequest: %v", err)
	}

	tools := declaredTools(req)
	if len(tools) != 1 {
		t.Fatalf("expected 1 declared tool, got %+v", tools)
	}
	schema := tools[0].InputSchema
	if schema["type"] != "object" {
		t.Fatalf("expected a real object schema (not the old generic {input: string}), got %+v", schema)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["query"]; !ok {
		t.Fatalf("expected the manifest's own \"query\" property to reach the model, got %+v", schema)
	}
}

// TestBuildPluginTool_Run_MarshalsStructuredArgsAndParsesJSONResult is the
// other half: the arguments a model actually sends (a plain map matching
// the declared schema, not a hand-wrapped JSON string) must reach the wasm
// call as clean JSON, and a JSON object the plugin returns must come back
// as real fields — not a second layer of string-wrapping either direction.
//
// This exercises callPluginTool directly (the helper BuildPluginTool's
// functiontool.New closure delegates to) rather than going through the
// tool.Tool's own Run — ADK's functiontool.Run calls ctx.ToolConfirmation()
// on its agent.ToolContext argument, which requires a real, non-nil
// agent.InvocationContext to construct and would make this test about
// ADK plumbing rather than about our marshal/unmarshal logic.
func TestBuildPluginTool_Run_MarshalsStructuredArgsAndParsesJSONResult(t *testing.T) {
	runtime := &fakePluginRuntime{output: []byte(`{"rows":[{"id":1}]}`)}
	opts := PluginRuntimeOptions{PluginID: "acme.sql"}

	result, err := callPluginTool(context.Background(), runtime, "acme.sql/run_query", "acme.sql@1.0.0", []byte{0x00, 0x61, 0x73, 0x6d}, opts, "run_query", map[string]any{"query": "select 1"})
	if err != nil {
		t.Fatalf("callPluginTool: %v", err)
	}

	var gotInput map[string]any
	if err := json.Unmarshal(runtime.input, &gotInput); err != nil {
		t.Fatalf("expected the wasm call's input to be clean JSON, got %q: %v", runtime.input, err)
	}
	if gotInput["query"] != "select 1" {
		t.Fatalf("expected structured args to reach the wasm call directly, got %+v", gotInput)
	}

	rows, ok := result["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected the plugin's JSON object result to come back as real fields, got %+v", result)
	}
}

func TestParsePluginEntry(t *testing.T) {
	fn, err := ParsePluginEntry("plugin.wasm#render_chart")
	if err != nil {
		t.Fatalf("ParsePluginEntry: %v", err)
	}
	if fn != "render_chart" {
		t.Fatalf("expected %q, got %q", "render_chart", fn)
	}

	if _, err := ParsePluginEntry("plugin.wasm"); err == nil {
		t.Fatal("expected an error for an entry with no \"#function\" suffix")
	}
	if _, err := ParsePluginEntry("plugin.wasm#"); err == nil {
		t.Fatal("expected an error for an entry with an empty function name")
	}
}
