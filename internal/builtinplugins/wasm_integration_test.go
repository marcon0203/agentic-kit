package builtinplugins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
)

// TestBuiltinConnectorWasm_EntriesResolve proves the embedded plugin.wasm
// both connector manifests point at is a real, loadable Extism module with
// every exported function their manifest.extensions.tools[] entries claim
// — the same automated gate (spec-20 §5.3) plugin.Service.SeedBuiltin runs
// via s.wasm.ValidateEntries, exercised here directly against the actual
// embedded bytes rather than a fake validator.
func TestBuiltinConnectorWasm_EntriesResolve(t *testing.T) {
	wasmBytes, err := postgresConnectorFS.ReadFile("postgresconnector/plugin.wasm")
	if err != nil {
		t.Fatal(err)
	}

	rt := extism.NewRuntime(nil)
	defer func() { _ = rt.Close(context.Background()) }()

	err = rt.ValidateEntries(context.Background(), "agentic-kit.postgres-connector@1.0.0", wasmBytes,
		[]string{"list_tables", "run_query", "run_write"})
	if err != nil {
		t.Fatalf("ValidateEntries: %v", err)
	}
}

// TestChartRendererWasm_RenderChart_ValidatesAndEchoesBack proves the
// chart renderer's embedded plugin.wasm (spec-20 §4.2 method A: a real
// tools[] call, not a fenced-code pattern match) is loadable and its
// render_chart export does what BuildPluginTool's Run expects of it —
// round-trip a schema-valid ChartSpec unchanged, and reject one whose
// dataset lengths don't match its labels.
func TestChartRendererWasm_RenderChart_ValidatesAndEchoesBack(t *testing.T) {
	wasmBytes, err := chartRendererFS.ReadFile("chartrenderer/plugin.wasm")
	if err != nil {
		t.Fatal(err)
	}

	rt := extism.NewRuntime(nil)
	defer func() { _ = rt.Close(context.Background()) }()

	if err := rt.ValidateEntries(context.Background(), "agentic-kit.chart-renderer@1.1.0", wasmBytes, []string{"render_chart"}); err != nil {
		t.Fatalf("ValidateEntries: %v", err)
	}

	valid := []byte(`{"type":"bar","title":"月度销量","labels":["一月","二月"],"datasets":[{"label":"销量","data":[120,200]}]}`)
	out, err := rt.Call(context.Background(), "agentic-kit.chart-renderer@1.1.0", wasmBytes, extism.Options{}, "render_chart", valid)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got["type"] != "bar" || got["title"] != "月度销量" {
		t.Fatalf("expected the spec echoed back unchanged, got %+v", got)
	}

	mismatched := []byte(`{"type":"bar","labels":["一月","二月"],"datasets":[{"label":"销量","data":[120]}]}`)
	if _, err := rt.Call(context.Background(), "agentic-kit.chart-renderer@1.1.0", wasmBytes, extism.Options{}, "render_chart", mismatched); err == nil {
		t.Fatal("expected an error for a dataset whose data length doesn't match labels")
	}
}
