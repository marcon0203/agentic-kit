package orchestrator

import "testing"

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
				map[string]any{"name": "render_chart", "entry": "plugin.wasm#render_chart", "description": "renders a chart"},
			},
		},
	}
	entry, description, ok := findExtensionEntry(manifest, "tools", "render_chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if entry != "plugin.wasm#render_chart" || description != "renders a chart" {
		t.Fatalf("unexpected result: entry=%q description=%q", entry, description)
	}

	if _, _, ok := findExtensionEntry(manifest, "tools", "does_not_exist"); ok {
		t.Fatal("expected no match for an unknown tool name")
	}
	if _, _, ok := findExtensionEntry(manifest, "connectors", "render_chart"); ok {
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
					"name": "chart", "entry": "ui/chart.html",
					"auto_render": map[string]any{"fenced_lang": []any{"chart", "acme-chart"}},
				},
			},
		},
	}
	entry, fencedLangs, ok := findRendererEntry(manifest, "chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if entry != "ui/chart.html" {
		t.Fatalf("unexpected entry: %q", entry)
	}
	if len(fencedLangs) != 2 || fencedLangs[0] != "chart" || fencedLangs[1] != "acme-chart" {
		t.Fatalf("unexpected fenced langs: %+v", fencedLangs)
	}

	if _, _, ok := findRendererEntry(manifest, "does_not_exist"); ok {
		t.Fatal("expected no match for an unknown renderer name")
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
