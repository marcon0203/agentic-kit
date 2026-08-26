package orchestrator

import (
	"encoding/base64"
	"testing"
)

func TestParsePluginRef(t *testing.T) {
	pluginID, name, ok := parsePluginRef("plugin:acme.charts/render_chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if pluginID != "acme.charts" || name != "render_chart" {
		t.Fatalf("unexpected parse: pluginID=%q name=%q", pluginID, name)
	}
}

func TestParsePluginRef_RejectsNonPluginRefs(t *testing.T) {
	for _, ref := range []string{"internal-search", "mcp:foo", "plugin:no-slash", ""} {
		if _, _, ok := parsePluginRef(ref); ok && ref != "plugin:no-slash" {
			t.Fatalf("expected ref %q to be rejected", ref)
		}
	}
	// "plugin:no-slash" has no "/" so Cut's ok is false too.
	if _, _, ok := parsePluginRef("plugin:no-slash"); ok {
		t.Fatal("expected a plugin ref with no \"/\" to be rejected")
	}
}

func TestFindExtensionEntry(t *testing.T) {
	manifest := map[string]any{
		"extensions": map[string]any{
			"tools": []any{
				map[string]any{
					"name": "render_chart", "entry": "plugin.wasm#render_chart", "description": "renders a chart",
					"input_schema": map[string]any{"type": "object", "required": []any{"query"}, "properties": map[string]any{"query": map[string]any{"type": "string"}}},
				},
			},
		},
	}
	entry, description, inputSchema, ok := findExtensionEntry(manifest, "tools", "render_chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if entry != "plugin.wasm#render_chart" || description != "renders a chart" {
		t.Fatalf("unexpected result: entry=%q description=%q", entry, description)
	}
	if inputSchema["type"] != "object" {
		t.Fatalf("expected input_schema to be passed through, got %+v", inputSchema)
	}

	if _, _, _, ok := findExtensionEntry(manifest, "tools", "does_not_exist"); ok {
		t.Fatal("expected no match for an unknown tool name")
	}
	if _, _, _, ok := findExtensionEntry(manifest, "connectors", "render_chart"); ok {
		t.Fatal("expected no match when looking in the wrong extension point")
	}
}

func TestFindToolUIEntry(t *testing.T) {
	manifest := map[string]any{
		"extensions": map[string]any{
			"tools": []any{
				map[string]any{"name": "render_chart", "entry": "plugin.wasm#render_chart", "ui": "ui/chart.html"},
			},
		},
	}
	if got := findToolUIEntry(manifest, "render_chart"); got != "ui/chart.html" {
		t.Fatalf("unexpected ui entry: %q", got)
	}
	if got := findToolUIEntry(manifest, "does_not_exist"); got != "" {
		t.Fatalf("expected empty ui entry for unknown tool, got %q", got)
	}
}

func TestFindRendererEntry(t *testing.T) {
	manifest := map[string]any{
		"extensions": map[string]any{
			"renderers": []any{
				map[string]any{
					"name": "chart", "entry": "ui/chart.html", "description": "emit a ```chart block shaped {labels, datasets}",
					"auto_render": map[string]any{"fenced_lang": []any{"chart", "acme-chart"}},
				},
			},
		},
	}
	entry, description, fencedLangs, ok := findRendererEntry(manifest, "chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if entry != "ui/chart.html" {
		t.Fatalf("unexpected entry: %q", entry)
	}
	if description != "emit a ```chart block shaped {labels, datasets}" {
		t.Fatalf("unexpected description: %q", description)
	}
	if len(fencedLangs) != 2 || fencedLangs[0] != "chart" || fencedLangs[1] != "acme-chart" {
		t.Fatalf("unexpected fenced langs: %+v", fencedLangs)
	}

	if _, _, _, ok := findRendererEntry(manifest, "does_not_exist"); ok {
		t.Fatal("expected no match for an unknown renderer name")
	}
}

func TestBindConnector_NoConnectorsConfigured(t *testing.T) {
	a := &resourceAuthorizer{connectors: nil}
	connRef, ok, err := a.bindConnector([]byte(`{"connector_resource_id":1}`))
	if err != nil || ok || connRef != "" {
		t.Fatalf("expected a silent no-op with no connectors backend, got (%q, %v, %v)", connRef, ok, err)
	}
}

func TestBindConnector_NoConnectorResourceIDInConfig(t *testing.T) {
	a := &resourceAuthorizer{connectors: NewConnectorRegistry(nil)}
	connRef, ok, err := a.bindConnector([]byte(`{}`))
	if err != nil || ok || connRef != "" {
		t.Fatalf("expected a silent no-op when the installation doesn't reference a connector, got (%q, %v, %v)", connRef, ok, err)
	}
}

func TestBindConnector_EmptyConfigIsNoop(t *testing.T) {
	a := &resourceAuthorizer{connectors: NewConnectorRegistry(nil)}
	connRef, ok, err := a.bindConnector(nil)
	if err != nil || ok || connRef != "" {
		t.Fatalf("expected a silent no-op for an empty installation config, got (%q, %v, %v)", connRef, ok, err)
	}
}

func TestBindConnector_MalformedResourceIDIsNoop(t *testing.T) {
	a := &resourceAuthorizer{connectors: NewConnectorRegistry(nil)}
	// Not valid base64("tool:<id>") — e.g. a stale/hand-edited install
	// config — must not be treated as a run failure.
	connRef, ok, err := a.bindConnector([]byte(`{"connector_resource_id":"not-a-real-id"}`))
	if err != nil || ok || connRef != "" {
		t.Fatalf("expected a silent no-op for a malformed connector_resource_id, got (%q, %v, %v)", connRef, ok, err)
	}
}

func TestDecodeToolResourceID(t *testing.T) {
	// Mirrors internal/api's encodeResourceID(resource.KindTool, 42) —
	// duplicated here rather than imported, see decodeToolResourceID's doc
	// comment for why.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("tool:42"))
	id, err := decodeToolResourceID(encoded)
	if err != nil {
		t.Fatalf("decodeToolResourceID: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
}

func TestDecodeToolResourceID_RejectsWrongKind(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("skill:42"))
	if _, err := decodeToolResourceID(encoded); err == nil {
		t.Fatal("expected an error for a non-tool resource id")
	}
}

func TestDecodeToolResourceID_RejectsNonBase64(t *testing.T) {
	if _, err := decodeToolResourceID("not base64!!"); err == nil {
		t.Fatal("expected an error for a non-base64 string")
	}
}

func TestStringFieldAndBoolField(t *testing.T) {
	m := map[string]any{"dialect": "postgres", "allow_write": true, "port": float64(5432)}
	if got := stringField(m, "dialect"); got != "postgres" {
		t.Errorf("stringField = %q, want \"postgres\"", got)
	}
	if got := stringField(m, "missing"); got != "" {
		t.Errorf("stringField for missing key = %q, want \"\"", got)
	}
	if got := boolField(m, "allow_write"); !got {
		t.Error("boolField = false, want true")
	}
	if got := boolField(m, "missing"); got {
		t.Error("boolField for missing key = true, want false")
	}
}

func TestRequiresNetwork(t *testing.T) {
	manifest := map[string]any{"requires": map[string]any{"network": []any{"api.acme.example", "cdn.acme.example"}}}
	got := requiresNetwork(manifest)
	if len(got) != 2 || got[0] != "api.acme.example" || got[1] != "cdn.acme.example" {
		t.Fatalf("unexpected result: %+v", got)
	}

	if got := requiresNetwork(map[string]any{}); len(got) != 0 {
		t.Fatalf("expected an empty (not nil-panicking) result for a manifest with no requires, got %+v", got)
	}
}
