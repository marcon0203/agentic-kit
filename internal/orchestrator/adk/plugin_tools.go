package adk

import (
	"context"
	"fmt"
	"strings"

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

type pluginToolArgs struct {
	Input string `json:"input" jsonschema_description:"Input passed to the plugin tool, as free-form text or JSON."`
}
type pluginToolResult struct {
	Output string `json:"output"`
}

// BuildPluginTool builds the ADK tool.Tool for one plugin `tools[]`
// extension entry. spec.Ref is the tool's registration name (what the
// model sees); spec.Config carries everything BuildPluginTool needs to
// actually invoke it, populated by the Authorizer.
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

	return functiontool.New(functiontool.Config{Name: SanitizePluginToolName(spec.Ref), Description: description},
		func(ctx agent.ToolContext, args pluginToolArgs) (pluginToolResult, error) {
			output, err := runtime.Call(ctx, wasmKey, wasmBytes, opts, funcName, []byte(args.Input))
			if err != nil {
				return pluginToolResult{}, fmt.Errorf("plugin tool %q: %w", spec.Ref, err)
			}
			return pluginToolResult{Output: string(output)}, nil
		})
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
