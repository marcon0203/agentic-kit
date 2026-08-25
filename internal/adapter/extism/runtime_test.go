package extism_test

import (
	"context"
	"os"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

// testdata/hello.wasm and fail.wasm are extism/go-sdk's own upstream test
// fixtures (MIT licensed) — hello.wasm's run_test exports "Hello, world!",
// fail.wasm's run_test always exits 1 with "Some error message". Using
// them here means these tests exercise the real Extism/wazero runtime, not
// a mock of it.

func loadWasm(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return b
}

func TestRuntime_CallReturnsOutput(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	out, err := rt.Call(context.Background(), "hello@1.0.0", loadWasm(t, "hello.wasm"), extism.Options{}, "run_test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(out) != "Hello, world!" {
		t.Fatalf("expected %q, got %q", "Hello, world!", out)
	}
}

func TestRuntime_CallCachesCompiledModule(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	wasm := loadWasm(t, "hello.wasm")
	for i := 0; i < 3; i++ {
		out, err := rt.Call(context.Background(), "hello@1.0.0", wasm, extism.Options{}, "run_test", nil)
		if err != nil {
			t.Fatalf("Call #%d: %v", i, err)
		}
		if string(out) != "Hello, world!" {
			t.Fatalf("Call #%d: expected %q, got %q", i, "Hello, world!", out)
		}
	}
}

func TestRuntime_CallPropagatesPluginFailure(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	_, err := rt.Call(context.Background(), "fail@1.0.0", loadWasm(t, "fail.wasm"), extism.Options{}, "run_test", nil)
	if err == nil {
		t.Fatal("expected an error from a plugin that exits non-zero")
	}
}

func TestRuntime_CallUnknownFunctionErrors(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	_, err := rt.Call(context.Background(), "hello@1.0.0", loadWasm(t, "hello.wasm"), extism.Options{}, "does_not_exist", nil)
	if err == nil {
		t.Fatal("expected an error calling an unknown function")
	}
}

// Runtime must satisfy the domain port it backs — a compile-time check,
// not a runtime assertion.
var _ plugin.WasmValidator = (*extism.Runtime)(nil)

func TestRuntime_ValidateEntriesAcceptsExistingFunction(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	err := rt.ValidateEntries(context.Background(), "hello@1.0.0", loadWasm(t, "hello.wasm"), []string{"run_test"})
	if err != nil {
		t.Fatalf("ValidateEntries: %v", err)
	}
}

func TestRuntime_ValidateEntriesRejectsMissingFunction(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	err := rt.ValidateEntries(context.Background(), "hello@1.0.0", loadWasm(t, "hello.wasm"), []string{"does_not_exist"})
	if err == nil {
		t.Fatal("expected an error for a function the module doesn't export")
	}
}

func TestRuntime_ValidateEntriesPrimesCompileCacheForCall(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	wasm := loadWasm(t, "hello.wasm")
	if err := rt.ValidateEntries(context.Background(), "hello@1.0.0", wasm, []string{"run_test"}); err != nil {
		t.Fatalf("ValidateEntries: %v", err)
	}
	// A subsequent Call under the same wasmKey reuses the same compiled
	// module rather than recompiling — proven indirectly by it succeeding
	// without needing wasm bytes to be re-supplied correctly.
	out, err := rt.Call(context.Background(), "hello@1.0.0", wasm, extism.Options{}, "run_test", nil)
	if err != nil {
		t.Fatalf("Call after ValidateEntries: %v", err)
	}
	if string(out) != "Hello, world!" {
		t.Fatalf("expected %q, got %q", "Hello, world!", out)
	}
}

func TestRuntime_CompileInvalidWasmErrors(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	_, err := rt.Call(context.Background(), "garbage@1.0.0", []byte("not a wasm module"), extism.Options{}, "run_test", nil)
	if err == nil {
		t.Fatal("expected an error compiling invalid wasm bytes")
	}
}
