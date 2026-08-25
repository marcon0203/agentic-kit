package dslschema

import (
	"encoding/json"
	"testing"
)

func TestPluginValidator_AcceptsAValidManifest(t *testing.T) {
	v, err := NewPluginValidator()
	if err != nil {
		t.Fatalf("NewPluginValidator: %v", err)
	}

	raw := `{
		"manifest_version": 1,
		"id": "acme.charts",
		"version": "1.2.0",
		"display_name": "图表渲染",
		"requires": {
			"host_api": ">=1.0 <2.0",
			"network": ["api.acme.example"],
			"permissions": ["read:run_output"]
		},
		"extensions": {
			"tools": [{"name": "render_chart", "entry": "plugin.wasm#render_chart"}],
			"renderers": [{"name": "chart", "entry": "ui/chart.html", "auto_render": {"fenced_lang": ["chart"]}}],
			"providers": [{"whatever": "an extension type this schema doesn't know about yet"}]
		}
	}`
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	errs, err := v.Validate(doc)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected a valid manifest (unknown extension types must pass through), got errors: %+v", errs)
	}
}

func TestPluginValidator_RejectsMalformedID(t *testing.T) {
	v, err := NewPluginValidator()
	if err != nil {
		t.Fatalf("NewPluginValidator: %v", err)
	}

	raw := `{
		"manifest_version": 1,
		"id": "notdotted",
		"version": "1.2.0",
		"display_name": "x",
		"requires": {"host_api": ">=1.0 <2.0"}
	}`
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	errs, err := v.Validate(doc)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a validation error for a non-reverse-domain id")
	}
}

func TestPluginValidator_RejectsMalformedHostAPIRange(t *testing.T) {
	v, err := NewPluginValidator()
	if err != nil {
		t.Fatalf("NewPluginValidator: %v", err)
	}

	raw := `{
		"manifest_version": 1,
		"id": "acme.charts",
		"version": "1.2.0",
		"display_name": "x",
		"requires": {"host_api": "garbage"}
	}`
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	errs, err := v.Validate(doc)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a validation error for a malformed host_api range")
	}
}

func TestPluginValidator_RejectsUnknownTopLevelField(t *testing.T) {
	v, err := NewPluginValidator()
	if err != nil {
		t.Fatalf("NewPluginValidator: %v", err)
	}

	raw := `{
		"manifest_version": 1,
		"id": "acme.charts",
		"version": "1.2.0",
		"display_name": "x",
		"requires": {"host_api": ">=1.0 <2.0"},
		"some_made_up_field": true
	}`
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	errs, err := v.Validate(doc)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a validation error for an unknown top-level field")
	}
}
