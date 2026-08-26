package orchestrator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// parsePluginRef splits a "plugin:{plugin_id}/{name}" capabilities ref
// (spec-20 §5.1: "不新增字段...按 plugin:acme.charts/render_chart 这样的
// ref 引用") into its plugin_id and extension-point name halves. name is
// a tools[]/connectors[]/hooks[] entry's own `name` field — which
// extension-point list it's found in is resolved by manifestExtensionPoint,
// not encoded in the ref itself.
func parsePluginRef(ref string) (pluginID, name string, ok bool) {
	rest, ok := strings.CutPrefix(ref, "plugin:")
	if !ok {
		return "", "", false
	}
	pluginID, name, ok = strings.Cut(rest, "/")
	return pluginID, name, ok
}

// authorizePlugin resolves one "plugin:{id}/{name}" ref against the
// caller's own plugin_installations. An uninstalled, disabled, or
// unresolvable ref is simply not authorized (ok=false) — the same
// "未授权的不进图" rule every other resource kind already follows
// (resourceAuthorizer.Authorize's own doc comment).
func (a *resourceAuthorizer) authorizePlugin(pluginID, name string) (adk.ToolSpec, bool, error) {
	inst, err := a.q.GetPluginInstallation(a.ctx, store.GetPluginInstallationParams{OwnerUserID: a.ownerID, PluginID: pluginID})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}
	if inst.Status != int16(plugin.StatusEnabled) {
		return adk.ToolSpec{}, false, nil
	}

	ver, err := a.q.GetPluginVersion(a.ctx, store.GetPluginVersionParams{PluginID: pluginID, Version: inst.Version})
	if errors.Is(err, pgx.ErrNoRows) {
		return adk.ToolSpec{}, false, nil
	}
	if err != nil {
		return adk.ToolSpec{}, false, err
	}

	var manifest map[string]any
	if err := json.Unmarshal(ver.Manifest, &manifest); err != nil {
		return adk.ToolSpec{}, false, err
	}

	if entry, description, inputSchema, ok := findExtensionEntry(manifest, "tools", name); ok {
		funcName, err := adk.ParsePluginEntry(entry)
		if err != nil {
			return adk.ToolSpec{}, false, err
		}

		wasmBytes, err := a.pluginWasm.Fetch(a.ctx, ver.OssPrefix)
		if err != nil {
			return adk.ToolSpec{}, false, err
		}

		config := map[string]any{
			adk.PluginConfigKeyWasmKey:           pluginID + "@" + inst.Version,
			adk.PluginConfigKeyWasmBytes:         wasmBytes,
			adk.PluginConfigKeyFuncName:          funcName,
			adk.PluginConfigKeyAllowedHosts:      requiresNetwork(manifest),
			adk.PluginRendererConfigKeyOSSPrefix: ver.OssPrefix,
		}
		if description != "" {
			config["description"] = description
		}
		if len(inputSchema) > 0 {
			config[adk.PluginConfigKeyInputSchema] = inputSchema
		}
		if uiEntry := findToolUIEntry(manifest, name); uiEntry != "" {
			config[adk.PluginConfigKeyUIEntry] = uiEntry
		}

		if connRef, ok, err := a.bindConnector(inst.Config); err != nil {
			return adk.ToolSpec{}, false, err
		} else if ok {
			config[adk.PluginConfigKeyPluginConfig] = map[string]string{"connection_ref": connRef}
		}

		return adk.ToolSpec{
			Ref: "plugin:" + pluginID + "/" + name, Kind: adk.KindPlugin,
			Config: config, OwnerID: a.ownerID,
		}, true, nil
	}

	// Not a tools[] entry — try renderers[] (spec-20 §4.2's auto_render
	// registration; connectors resolve through tools[]'s own
	// connector_resource_id binding above, P3, not a separate ref kind).
	if entry, fencedLangs, ok := findRendererEntry(manifest, name); ok {
		return adk.ToolSpec{
			Ref: "plugin:" + pluginID + "/" + name, Kind: adk.KindPluginRenderer,
			Config: map[string]any{
				adk.PluginRendererConfigKeyPluginID:    pluginID,
				adk.PluginRendererConfigKeyVersion:     inst.Version,
				adk.PluginRendererConfigKeyName:        name,
				adk.PluginRendererConfigKeyOSSPrefix:   ver.OssPrefix,
				adk.PluginRendererConfigKeyEntry:       entry,
				adk.PluginRendererConfigKeyFencedLangs: fencedLangs,
			},
			OwnerID: a.ownerID,
		}, true, nil
	}

	// Not a renderers[] entry either — try hooks[] (spec-20 §4.4's
	// compile-time takeover of capabilities.hooks' five fields). name here
	// is the hook point (before_tool_call, ...), matched against the
	// manifest's hooks[] entries by their own `point` field, not a `name`
	// field — hooks[] has no name, one plugin declares at most one entry
	// per point.
	if entry, ok := findHookEntry(manifest, name); ok {
		funcName, err := adk.ParsePluginEntry(entry)
		if err != nil {
			return adk.ToolSpec{}, false, err
		}
		wasmBytes, err := a.pluginWasm.Fetch(a.ctx, ver.OssPrefix)
		if err != nil {
			return adk.ToolSpec{}, false, err
		}
		return adk.ToolSpec{
			Ref: "plugin:" + pluginID + "/" + name, Kind: adk.KindPluginHook,
			Config: map[string]any{
				adk.PluginHookConfigKeyPluginID:    pluginID,
				adk.PluginHookConfigKeyVersion:     inst.Version,
				adk.PluginHookConfigKeyPoint:       name,
				adk.PluginHookConfigKeyEntry:       entry,
				adk.PluginHookConfigKeyOSSPrefix:   ver.OssPrefix,
				adk.PluginHookConfigKeyFuncName:    funcName,
				adk.PluginHookConfigKeyWasmBytes:   wasmBytes,
				adk.PluginHookConfigKeyPermissions: manifestPermissions(manifest),
			},
			OwnerID: a.ownerID,
		}, true, nil
	}

	return adk.ToolSpec{}, false, nil
}

// bindConnector reads an installation's optional connector_resource_id
// (set via the existing PATCH /plugins/{id}/install config, spec-20 §4.3),
// resolves it against the caller's own tools resource, decrypts it, and
// binds a real connection — returning the opaque connection_ref a plugin's
// sql.* host function calls will use. ok=false when the installation
// simply doesn't use a connector, which is the common case and not an
// error. The bound ref is tracked so ReleaseBound can close it once the
// run that authorized it is done (spec-20 §4.5's "谁创建谁回收").
func (a *resourceAuthorizer) bindConnector(rawInstConfig []byte) (connRef string, ok bool, err error) {
	if a.connectors == nil || len(rawInstConfig) == 0 {
		return "", false, nil
	}
	var instConfig struct {
		ConnectorResourceID string `json:"connector_resource_id"`
	}
	if err := json.Unmarshal(rawInstConfig, &instConfig); err != nil || instConfig.ConnectorResourceID == "" {
		return "", false, nil
	}
	resourceID, err := decodeToolResourceID(instConfig.ConnectorResourceID)
	if err != nil {
		// A malformed id (a stale/hand-edited install config, say) is the
		// same as "this installation doesn't use a connector" — not a run
		// failure, since nothing about the run itself is wrong.
		return "", false, nil
	}

	row, err := a.q.GetToolByIDForOwner(a.ctx, store.GetToolByIDForOwnerParams{ID: resourceID, OwnerUserID: a.ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if row.Status != int16(resource.StatusEnabled) {
		return "", false, nil
	}

	decrypted, err := a.decryptConfig(row.Config)
	if err != nil {
		return "", false, err
	}
	cfg := ConnectorConfig{
		Dialect:    stringField(decrypted, "dialect"),
		Host:       stringField(decrypted, "host"),
		Database:   stringField(decrypted, "database"),
		Username:   stringField(decrypted, "username"),
		Password:   stringField(decrypted, "password"),
		AllowWrite: boolField(decrypted, "allow_write"),
	}
	if port, ok := decrypted["port"].(float64); ok {
		cfg.Port = int(port)
	}

	connRef, err = a.connectors.Bind(a.ctx, cfg)
	if err != nil {
		return "", false, err
	}
	a.boundMu.Lock()
	a.boundRefs = append(a.boundRefs, connRef)
	a.boundMu.Unlock()
	return connRef, true, nil
}

// decodeToolResourceID reverses internal/api's encodeResourceID for a
// "tool"-kind resource — the frontend never sees a resource's raw numeric
// row id, only this base64("tool:<id>") string (the same one POST
// /resources returns as a resource's own `id` field), so a
// connector_resource_id set via PATCH /plugins/{id}/install's config
// arrives in this encoded form too. Duplicated here rather than exported
// from internal/api because it's a two-line encoding detail, not a
// dependency worth taking on the transport package from the orchestrator
// adapter.
func decodeToolResourceID(external string) (int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(external)
	if err != nil {
		return 0, fmt.Errorf("resource id: not base64")
	}
	kind, idStr, ok := strings.Cut(string(decoded), ":")
	if !ok || kind != "tool" {
		return 0, fmt.Errorf("resource id: not a tool resource")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("resource id: bad numeric part")
	}
	return id, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func boolField(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// findToolUIEntry reads a tools[] entry's optional `ui` field.
func findToolUIEntry(manifest map[string]any, name string) string {
	extensions, _ := manifest["extensions"].(map[string]any)
	items, _ := extensions["tools"].([]any)
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != name {
			continue
		}
		ui, _ := m["ui"].(string)
		return ui
	}
	return ""
}

// findRendererEntry looks up one renderers[] item by name, returning its
// entry path and auto_render.fenced_lang list.
func findRendererEntry(manifest map[string]any, name string) (entry string, fencedLangs []string, ok bool) {
	extensions, _ := manifest["extensions"].(map[string]any)
	items, _ := extensions["renderers"].([]any)
	for _, item := range items {
		m, mok := item.(map[string]any)
		if !mok {
			continue
		}
		if n, _ := m["name"].(string); n != name {
			continue
		}
		entry, _ = m["entry"].(string)
		if entry == "" {
			return "", nil, false
		}
		autoRender, _ := m["auto_render"].(map[string]any)
		rawLangs, _ := autoRender["fenced_lang"].([]any)
		for _, v := range rawLangs {
			if s, ok := v.(string); ok {
				fencedLangs = append(fencedLangs, s)
			}
		}
		return entry, fencedLangs, true
	}
	return "", nil, false
}

// findExtensionEntry looks up one named item in manifest.extensions[point]
// (schemas/plugin.schema.json's tools/connectors array shape — both use
// {name, entry, description?}), returning its entry string, optional
// description, and optional input_schema (tools[] only — connectors[]
// entries never carry one, so this is simply nil for that point).
func findExtensionEntry(manifest map[string]any, point, name string) (entry, description string, inputSchema map[string]any, ok bool) {
	extensions, _ := manifest["extensions"].(map[string]any)
	items, _ := extensions[point].([]any)
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != name {
			continue
		}
		entry, _ = m["entry"].(string)
		description, _ = m["description"].(string)
		inputSchema, _ = m["input_schema"].(map[string]any)
		return entry, description, inputSchema, entry != ""
	}
	return "", "", nil, false
}

// requiresNetwork reads manifest.requires.network — the AllowedHosts
// whitelist Extism enforces (spec-20 §4.1).
func requiresNetwork(manifest map[string]any) []string {
	requires, _ := manifest["requires"].(map[string]any)
	raw, _ := requires["network"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// findHookEntry looks up a hooks[] item by its `point` field (schemas/
// plugin.schema.json: {point, entry} — no `name`, unlike tools/renderers)
// and returns its entry string.
func findHookEntry(manifest map[string]any, point string) (entry string, ok bool) {
	extensions, _ := manifest["extensions"].(map[string]any)
	items, _ := extensions["hooks"].([]any)
	for _, item := range items {
		m, mok := item.(map[string]any)
		if !mok {
			continue
		}
		if p, _ := m["point"].(string); p != point {
			continue
		}
		entry, _ = m["entry"].(string)
		return entry, entry != ""
	}
	return "", false
}

// manifestPermissions reads manifest.requires.permissions — what
// HookRegistration.CanWrite checks a hook's declared write:* permission
// against (spec-20 §4.4).
func manifestPermissions(manifest map[string]any) []string {
	requires, _ := manifest["requires"].(map[string]any)
	raw, _ := requires["permissions"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
