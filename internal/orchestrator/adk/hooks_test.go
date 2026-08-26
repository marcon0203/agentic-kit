package adk

import (
	"context"
	"errors"
	"testing"
)

func hookSpec(pluginID, point string, permissions []string) ToolSpec {
	return ToolSpec{
		Ref: "plugin:" + pluginID + "/" + point, Kind: KindPluginHook,
		Config: map[string]any{
			PluginHookConfigKeyPluginID:    pluginID,
			PluginHookConfigKeyVersion:     "1.0.0",
			PluginHookConfigKeyPoint:       point,
			PluginHookConfigKeyEntry:       "plugin.wasm#" + point,
			PluginHookConfigKeyFuncName:    point,
			PluginHookConfigKeyPermissions: permissions,
		},
	}
}

func TestCompileHooks_ResolvesOneHookPerPoint(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{
			"hooks": map[string]any{
				"before_tool_call": []any{"plugin:acme.audit/before_tool_call"},
			},
		},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"plugin:acme.audit/before_tool_call": hookSpec("acme.audit", "before_tool_call", nil),
	}}

	var hooks []HookRegistration
	if err := compileHooks(context.Background(), def, authorizer, &hooks); err != nil {
		t.Fatalf("compileHooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].PluginID != "acme.audit" || hooks[0].Point != "before_tool_call" {
		t.Fatalf("unexpected hooks: %+v", hooks)
	}
}

func TestCompileHooks_RejectsTwoPluginsClaimingSamePoint(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{
			"hooks": map[string]any{
				"before_tool_call": []any{"plugin:acme.audit/before_tool_call", "plugin:other.audit/before_tool_call"},
			},
		},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"plugin:acme.audit/before_tool_call":  hookSpec("acme.audit", "before_tool_call", nil),
		"plugin:other.audit/before_tool_call": hookSpec("other.audit", "before_tool_call", nil),
	}}

	var hooks []HookRegistration
	err := compileHooks(context.Background(), def, authorizer, &hooks)
	if err == nil {
		t.Fatal("expected a compile error when two plugins claim the same hook point")
	}
}

func TestCompileHooks_RejectsPointMismatch(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{
			// The ref is filed under after_tool_call, but its manifest
			// entry actually declares point=before_tool_call.
			"hooks": map[string]any{
				"after_tool_call": []any{"plugin:acme.audit/before_tool_call"},
			},
		},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{
		"plugin:acme.audit/before_tool_call": hookSpec("acme.audit", "before_tool_call", nil),
	}}

	var hooks []HookRegistration
	if err := compileHooks(context.Background(), def, authorizer, &hooks); err == nil {
		t.Fatal("expected an error when a hook's declared point doesn't match the DSL field it's listed under")
	}
}

func TestCompileHooks_UnauthorizedRefSilentlySkipped(t *testing.T) {
	def := map[string]any{
		"capabilities": map[string]any{
			"hooks": map[string]any{"on_error": []any{"plugin:not.installed/on_error"}},
		},
	}
	authorizer := &fakeAuthorizer{authorized: map[string]ToolSpec{}}

	var hooks []HookRegistration
	if err := compileHooks(context.Background(), def, authorizer, &hooks); err != nil {
		t.Fatalf("compileHooks: %v", err)
	}
	if len(hooks) != 0 {
		t.Fatalf("expected no hooks, got %+v", hooks)
	}
}

func TestCompileHooks_NoHooksIsNoop(t *testing.T) {
	def := map[string]any{"capabilities": map[string]any{}}
	var hooks []HookRegistration
	if err := compileHooks(context.Background(), def, &fakeAuthorizer{}, &hooks); err != nil {
		t.Fatalf("compileHooks: %v", err)
	}
	if hooks != nil {
		t.Fatalf("expected nil hooks, got %+v", hooks)
	}
}

func TestHookRegistration_CanWrite(t *testing.T) {
	withPermission := HookRegistration{Point: "before_response", Permissions: []string{"write:response"}}
	if !withPermission.CanWrite() {
		t.Error("expected CanWrite to be true when the point's write permission is declared")
	}

	readOnly := HookRegistration{Point: "before_response", Permissions: []string{"read:response"}}
	if readOnly.CanWrite() {
		t.Error("expected CanWrite to be false when only a read permission is declared")
	}

	none := HookRegistration{Point: "before_response"}
	if none.CanWrite() {
		t.Error("expected CanWrite to be false with no permissions declared")
	}

	unknownPoint := HookRegistration{Point: "no_such_point", Permissions: []string{"write:response"}}
	if unknownPoint.CanWrite() {
		t.Error("expected CanWrite to be false for a point with no known write permission")
	}
}

type fakeHookPluginRuntime struct {
	output []byte
	err    error
	calls  int
}

func (f *fakeHookPluginRuntime) Call(_ context.Context, _ string, _ []byte, _ PluginRuntimeOptions, _ string, _ []byte) ([]byte, error) {
	f.calls++
	return f.output, f.err
}

func TestInvokeHook_NilRuntimeFailsGracefully(t *testing.T) {
	reg := HookRegistration{PluginID: "acme.audit", FuncName: "before_tool_call", WasmBytes: []byte("wasm")}
	if _, ok := invokeHook(context.Background(), reg, nil, map[string]any{}); ok {
		t.Error("expected invokeHook to report ok=false with a nil runtime")
	}
}

func TestInvokeHook_MissingWasmBytesFailsGracefully(t *testing.T) {
	reg := HookRegistration{PluginID: "acme.audit", FuncName: "before_tool_call"}
	rt := &fakeHookPluginRuntime{output: []byte(`{}`)}
	if _, ok := invokeHook(context.Background(), reg, rt, map[string]any{}); ok {
		t.Error("expected invokeHook to report ok=false with no wasm bytes to call")
	}
	if rt.calls != 0 {
		t.Error("expected invokeHook not to call the runtime with no wasm bytes")
	}
}

func TestInvokeHook_RuntimeErrorDegradesGracefully(t *testing.T) {
	reg := HookRegistration{PluginID: "acme.audit", FuncName: "before_tool_call", WasmBytes: []byte("wasm")}
	rt := &fakeHookPluginRuntime{err: errors.New("wasm trap")}
	result, ok := invokeHook(context.Background(), reg, rt, map[string]any{})
	if ok || result != nil {
		t.Errorf("expected a runtime error to degrade to ok=false, got (%+v, %v)", result, ok)
	}
}

func TestInvokeHook_NonJSONOutputDegradesGracefully(t *testing.T) {
	reg := HookRegistration{PluginID: "acme.audit", FuncName: "before_tool_call", WasmBytes: []byte("wasm")}
	rt := &fakeHookPluginRuntime{output: []byte("not json")}
	if _, ok := invokeHook(context.Background(), reg, rt, map[string]any{}); ok {
		t.Error("expected non-JSON output to degrade to ok=false")
	}
}

func TestInvokeHook_SuccessReturnsDecodedResult(t *testing.T) {
	reg := HookRegistration{PluginID: "acme.audit", Version: "1.0.0", FuncName: "before_tool_call", WasmBytes: []byte("wasm")}
	rt := &fakeHookPluginRuntime{output: []byte(`{"args":{"x":1}}`)}
	result, ok := invokeHook(context.Background(), reg, rt, map[string]any{"tool": "t"})
	if !ok {
		t.Fatal("expected invokeHook to succeed")
	}
	args, _ := result["args"].(map[string]any)
	if args["x"] != float64(1) {
		t.Errorf("unexpected result: %+v", result)
	}
	if rt.calls != 1 {
		t.Errorf("expected exactly one runtime call, got %d", rt.calls)
	}
}
