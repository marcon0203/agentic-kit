package extism_test

import (
	"context"
	"os"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
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

func TestRuntime_CompileInvalidWasmErrors(t *testing.T) {
	rt := extism.NewRuntime()
	defer func() { _ = rt.Close(context.Background()) }()

	_, err := rt.Call(context.Background(), "garbage@1.0.0", []byte("not a wasm module"), extism.Options{}, "run_test", nil)
	if err == nil {
		t.Fatal("expected an error compiling invalid wasm bytes")
	}
}
