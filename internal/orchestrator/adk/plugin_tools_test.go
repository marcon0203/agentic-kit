package adk

import (
	"context"
	"testing"
)

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
	if tl.Name() != "acme.charts/render_chart" {
		t.Fatalf("unexpected tool name: %q", tl.Name())
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
