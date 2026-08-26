package adk

// HookPoints are the five capabilities.hooks fields schemas/agent.schema.json
// defines (spec-20 §4.4) — the fixed set a plugin's hooks[] entry may
// declare via its `point` field, in the exact order the runtime lifecycle
// visits them.
var HookPoints = []string{
	"before_tool_call",
	"after_tool_call",
	"before_response",
	"after_response",
	"on_error",
}

// hookWritePermission is the requires.permissions string a hook's manifest
// must declare before it is allowed to actually mutate anything at that
// point (spec-20 §4.4) — a hook that only declares the matching read:*
// permission (or nothing) still runs, but HookRegistration.CanWrite
// reports false so the caller knows to treat its output as observation
// only, never a rewrite.
var hookWritePermission = map[string]string{
	"before_tool_call": "write:tool_input",
	"after_tool_call":  "write:tool_output",
	"before_response":  "write:response",
	"after_response":   "write:response",
	"on_error":         "write:error",
}

// HookRegistration is one plugin hook an Agent's capabilities.hooks{}
// authorized for this run (spec-20 §4.4). Unlike a renderer, at most one
// plugin may ever own a given Point for one compiled Agent — compileTools
// rejects the Agent definition outright if two different plugins both
// declare the same point, rather than picking one ("no arbitration",
// unlike renderers' first-declared-wins).
type HookRegistration struct {
	PluginID  string
	Version   string
	Point     string
	Entry     string
	OSSPrefix string
	// FuncName/WasmBytes are what invokeHook actually calls — resolved and
	// fetched by the Authorizer at the same time as everything else
	// (Entry's "file#function" is parsed once, up front, same as a
	// callable tool's entry).
	FuncName  string
	WasmBytes []byte
	// Permissions is the plugin manifest's requires.permissions list,
	// carried through so the runtime can decide whether this hook's
	// output is actually applied (spec-20 §4.4: read-only permission or
	// none still runs the hook, but its response is observed, not
	// applied).
	Permissions []string
}

// CanWrite reports whether this hook declared the permission its Point
// requires to actually mutate anything — the runtime call site decides
// what "mutate" means at each point, this only answers "is it allowed to."
func (h HookRegistration) CanWrite() bool {
	want, ok := hookWritePermission[h.Point]
	if !ok {
		return false
	}
	for _, p := range h.Permissions {
		if p == want {
			return true
		}
	}
	return false
}

// Plugin hook ToolSpec.Config keys the Authorizer populates when a ref
// resolves to a hooks[] entry (KindPluginHook) rather than a tools[] or
// renderers[] one.
const (
	PluginHookConfigKeyPluginID    = "hook_plugin_id"
	PluginHookConfigKeyVersion     = "hook_version"
	PluginHookConfigKeyPoint       = "hook_point"
	PluginHookConfigKeyEntry       = "hook_entry"
	PluginHookConfigKeyOSSPrefix   = "hook_oss_prefix"
	PluginHookConfigKeyFuncName    = "hook_func_name"
	PluginHookConfigKeyWasmBytes   = "hook_wasm_bytes"  // []byte
	PluginHookConfigKeyPermissions = "hook_permissions" // []string
)

// HookRegistrationFromSpec builds a HookRegistration from a KindPluginHook
// ToolSpec.
func HookRegistrationFromSpec(spec ToolSpec) (HookRegistration, bool) {
	pluginID, _ := spec.Config[PluginHookConfigKeyPluginID].(string)
	point, _ := spec.Config[PluginHookConfigKeyPoint].(string)
	if pluginID == "" || point == "" {
		return HookRegistration{}, false
	}
	version, _ := spec.Config[PluginHookConfigKeyVersion].(string)
	entry, _ := spec.Config[PluginHookConfigKeyEntry].(string)
	ossPrefix, _ := spec.Config[PluginHookConfigKeyOSSPrefix].(string)
	funcName, _ := spec.Config[PluginHookConfigKeyFuncName].(string)
	wasmBytes, _ := spec.Config[PluginHookConfigKeyWasmBytes].([]byte)
	permissions, _ := spec.Config[PluginHookConfigKeyPermissions].([]string)
	return HookRegistration{
		PluginID: pluginID, Version: version, Point: point,
		Entry: entry, OSSPrefix: ossPrefix, FuncName: funcName, WasmBytes: wasmBytes,
		Permissions: permissions,
	}, true
}
