package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// PluginRuntime executes one compiled plugin's WASM function. The concrete
// implementation (internal/adapter/extism) owns compilation/caching, the
// sandbox's timeout/memory ceilings, and AllowedHosts — this package only
// ever sees "give me bytes out for bytes in," which is what keeps the ADK
// wiring itself free of a WASM-runtime dependency (spec-10's "所有 ADK 调用
// 收敛在 internal/orchestrator/adk 包内" cuts the other way here: it's the
// *host functions and sandboxing* that must stay out of this package, not
// the plugin concept itself).
type PluginRuntime interface {
	Call(ctx context.Context, wasmKey string, wasmBytes []byte, opts PluginRuntimeOptions, funcName string, input []byte) ([]byte, error)
}

// PluginRuntimeOptions mirrors internal/adapter/extism.Options without
// this package importing that adapter directly.
type PluginRuntimeOptions struct {
	AllowedHosts   []string
	TimeoutMS      uint64
	MaxMemoryPages uint32
	Config         map[string]string
	// PluginID/OwnerID identify who is making this call — the runtime uses
	// them to scope the kv.get/set host function's namespace (spec-20
	// §4.3's "按 (plugin, owner) 隔离"). Deliberately not something a
	// plugin's own request payload can carry: the runtime derives them
	// from the call itself, so a plugin cannot simply name a different
	// namespace to reach another installation's kv data.
	PluginID string
	OwnerID  int64
}

// Plugin ToolSpec.Config keys (spec.Kind == KindPlugin). Populated by
// whatever resolves a "plugin:{plugin_id}/{tool_name}" capabilities ref —
// spec-20 §5.1: "不新增字段", so a plugin's tools are referenced in the
// same capabilities.tools[] array as an ordinary resource ref, and it's
// the Authorizer, not the Agent DSL, that tells them apart.
const (
	PluginConfigKeyWasmKey        = "wasm_key"   // "{plugin_id}@{version}" — the compile-cache key
	PluginConfigKeyWasmBytes      = "wasm_bytes" // []byte, the plugin package's compiled module
	PluginConfigKeyFuncName       = "func_name"  // exported wasm function to call
	PluginConfigKeyAllowedHosts   = "allowed_hosts"
	PluginConfigKeyTimeoutMS      = "timeout_ms"
	PluginConfigKeyMaxMemoryPages = "max_memory_pages"
	PluginConfigKeyPluginConfig   = "plugin_config" // map[string]string, e.g. granted permissions
	// PluginConfigKeyUIEntry is set on a KindPlugin (callable tool) spec
	// when its manifest tools[].ui declares a UI resource entry (spec-20
	// §4.2 method A) — compileTools reads this to also register a
	// RendererRegistration alongside building the callable tool.
	PluginConfigKeyUIEntry = "ui_entry"
	// PluginConfigKeyInputSchema carries a tools[] entry's own
	// input_schema (schemas/plugin.schema.json, a standard JSON Schema
	// object, map[string]any) straight through to the ADK tool
	// declaration the model sees — without this, every plugin tool
	// declares the same generic {"input": "free-form text or JSON"}
	// parameter regardless of what the manifest actually asks for, and
	// the model has to guess (usually wrong, usually more than once) how
	// to shape a request like run_query's {"query": "..."}.
	PluginConfigKeyInputSchema = "input_schema"
)

// ParsePluginEntry splits a manifest tools[].entry string
// ("plugin.wasm#render_chart", schemas/plugin.schema.json) into its
// exported function name. The module filename half is documentation only
// in v1 — one plugin package compiles to exactly one wasm module, so
// there's nothing to select between; keeping the "#function" suffix
// mandatory now means a future multi-module package doesn't need a
// manifest format change to start using the file half for real.
func ParsePluginEntry(entry string) (funcName string, err error) {
	_, fn, ok := strings.Cut(entry, "#")
	if !ok || fn == "" {
		return "", fmt.Errorf("plugin entry %q: expected \"<file>#<function>\"", entry)
	}
	return fn, nil
}

// BuildPluginTool builds the ADK tool.Tool for one plugin `tools[]`
// extension entry. spec.Ref is the tool's registration name (what the
// model sees); spec.Config carries everything BuildPluginTool needs to
// actually invoke it, populated by the Authorizer.
//
// Arguments and results travel as plain map[string]any, not a fixed Go
// struct: a plugin declares its own input_schema in plugin.json, so the
// declaration the model sees — and the JSON bytes the WASM side actually
// receives — must match whatever that plugin asked for (run_query's
// {"query": "..."}, list_tables' no arguments at all, or anything a
// third-party plugin author's own input_schema describes), not a single
// generic "input" string every plugin was forced to squeeze itself into
// before this.
func BuildPluginTool(spec ToolSpec, runtime PluginRuntime) (tool.Tool, error) {
	if runtime == nil {
		return nil, fmt.Errorf("plugin tool %q: no plugin runtime configured", spec.Ref)
	}
	wasmKey, _ := spec.Config[PluginConfigKeyWasmKey].(string)
	wasmBytes, _ := spec.Config[PluginConfigKeyWasmBytes].([]byte)
	funcName, _ := spec.Config[PluginConfigKeyFuncName].(string)
	if wasmKey == "" || len(wasmBytes) == 0 || funcName == "" {
		return nil, fmt.Errorf("plugin tool %q: missing wasm_key/wasm_bytes/func_name", spec.Ref)
	}

	allowedHosts, _ := spec.Config[PluginConfigKeyAllowedHosts].([]string)
	pluginID, _, _ := strings.Cut(wasmKey, "@")
	opts := PluginRuntimeOptions{AllowedHosts: allowedHosts, PluginID: pluginID, OwnerID: spec.OwnerID}
	if ms, ok := spec.Config[PluginConfigKeyTimeoutMS].(uint64); ok {
		opts.TimeoutMS = ms
	}
	if pages, ok := spec.Config[PluginConfigKeyMaxMemoryPages].(uint32); ok {
		opts.MaxMemoryPages = pages
	}
	if cfg, ok := spec.Config[PluginConfigKeyPluginConfig].(map[string]string); ok {
		opts.Config = cfg
	}

	description, _ := spec.Config["description"].(string)
	if description == "" {
		description = fmt.Sprintf("Calls the %q plugin tool.", spec.Ref)
	}

	cfg := functiontool.Config{Name: SanitizePluginToolName(spec.Ref), Description: description}
	if raw, ok := spec.Config[PluginConfigKeyInputSchema].(map[string]any); ok && len(raw) > 0 {
		schema, err := manifestInputSchema(raw)
		if err != nil {
			return nil, fmt.Errorf("plugin tool %q: manifest input_schema: %w", spec.Ref, err)
		}
		cfg.InputSchema = schema
	}

	return functiontool.New(cfg,
		func(ctx agent.ToolContext, args map[string]any) (map[string]any, error) {
			return callPluginTool(ctx, runtime, spec.Ref, wasmKey, wasmBytes, opts, funcName, args)
		})
}

// callPluginTool does the actual marshal-call-unmarshal work behind a
// plugin tool's Run. Split out from BuildPluginTool's closure so it can be
// exercised directly in tests without going through ADK's functiontool.Run
// — which requires a real, non-nil agent.ToolContext (it calls
// ctx.ToolConfirmation()), unlike the ProcessRequest path.
func callPluginTool(ctx context.Context, runtime PluginRuntime, ref, wasmKey string, wasmBytes []byte, opts PluginRuntimeOptions, funcName string, args map[string]any) (map[string]any, error) {
	input, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("plugin tool %q: encode arguments: %w", ref, err)
	}
	// Diagnostic for "unknown or expired connection_ref" reports: this is
	// the exact connection_ref (if any) this specific call is about to
	// send the plugin, alongside the wasmKey it's compiled under — lets a
	// report be matched against connector_registry.go's connector_bound/
	// connector_released/connector_unknown_ref log lines to see whether a
	// call ever had the right ref to begin with, or whether it had one but
	// the connection was already gone by the time it ran.
	slog.Info("plugin_tool_call", "ref", ref, "wasm_key", wasmKey, "func_name", funcName, "connection_ref", opts.Config["connection_ref"])
	output, err := runtime.Call(ctx, wasmKey, wasmBytes, opts, funcName, input)
	if err != nil {
		return nil, fmt.Errorf("plugin tool %q: %w", ref, err)
	}
	// A plugin's export normally returns a JSON object (the sample
	// SQL connector's ToolOutput{rows}/WriteOutput{affected_rows},
	// see examples/plugins/sql-connector) — hand that back as-is so
	// the model sees real field names instead of one more layer of
	// string-wrapping. A plugin that returns plain text/non-JSON
	// still gets a usable result via the "output" fallback key.
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil || result == nil {
		return map[string]any{"output": string(output)}, nil
	}
	return result, nil
}

// manifestInputSchema converts a tools[] entry's input_schema — already
// decoded as a generic map[string]any from the manifest's own JSON — into
// the *jsonschema.Schema type functiontool.Config.InputSchema expects.
// Round-tripping through json.Marshal/Unmarshal (rather than a field-by-
// field conversion) is deliberate: jsonschema.Schema implements its own
// UnmarshalJSON to handle JSON Schema's polymorphic bits (type vs types,
// etc.), and re-deriving that logic by hand would just be a second,
// drifting copy of it.
func manifestInputSchema(raw map[string]any) (*jsonschema.Schema, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		return nil, err
	}
	return &schema, nil
}

// SanitizePluginToolName turns a "plugin:{id}/{tool}" ref into a name most
// model providers' function-calling APIs will actually accept — the colon
// and slash a plugin ref is built from aren't universally legal in a tool
// name, so this is what a plugin tool is really registered under, and what
// EventNodeToolCallFinished.Payload["name"] reports back. Not reversible by
// design: nothing needs to recover the original ref from the sanitized
// name, only compare two sanitized names for equality (see MatchToolRender).
func SanitizePluginToolName(ref string) string {
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
