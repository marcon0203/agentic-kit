package builtinplugins

import (
	"context"
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
