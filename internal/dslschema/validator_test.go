package dslschema

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// loadYAMLAsAny reads a YAML fixture and decodes it into the
// map[string]interface{} shape jsonschema expects — matching how a real
// request body arrives (json.Unmarshal into `any` also produces
// map[string]interface{}), unlike yaml.v3's default map[string]interface{}
// with string keys already, so no conversion needed here.
func loadYAMLAsAny(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return normalizeYAML(doc)
}

// normalizeYAML converts yaml.v3's map[string]interface{} (already
// string-keyed, unlike yaml.v2) recursively so nested maps match what
// jsonschema expects throughout — yaml.v3 already produces string keys, so
// this just walks slices/maps to be safe against any interface{} nesting.
func normalizeYAML(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = normalizeYAML(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeYAML(vv)
		}
		return out
	default:
		return val
	}
}

func fixturesDir(t *testing.T) string {
	t.Helper()
	// internal/dslschema -> repo root -> schemas/examples
	dir, err := filepath.Abs("../../schemas/examples")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return dir
}

func TestAgentValidator_AcceptsAllExampleFixtures(t *testing.T) {
	validator, err := NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	examples := []string{"architect.agent.yaml", "product_manager.agent.yaml", "ui_designer.agent.yaml", "fullstack_engineer.agent.yaml"}
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			doc := loadYAMLAsAny(t, filepath.Join(fixturesDir(t), name))
			errs, err := validator.Validate(doc)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if len(errs) != 0 {
				t.Fatalf("expected fixture %s to pass, got errors: %+v", name, errs)
			}
		})
	}
}

func TestAgentValidator_RejectsInvalidProvider(t *testing.T) {
	validator, err := NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := loadYAMLAsAny(t, filepath.Join(fixturesDir(t), "architect.agent.yaml"))
	m := doc.(map[string]any)
	model := m["model"].(map[string]any)
	model["provider"] = "not-a-real-provider"

	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected an invalid provider to be rejected")
	}

	found := false
	for _, e := range errs {
		if e.Field == "model.provider" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a field error on model.provider, got: %+v", errs)
	}
}

func TestAgentValidator_RejectsMissingRequiredField(t *testing.T) {
	validator, err := NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := loadYAMLAsAny(t, filepath.Join(fixturesDir(t), "architect.agent.yaml"))
	delete(doc.(map[string]any), "persona")

	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a missing required field to be rejected")
	}
}

func TestBundleValidator_AcceptsExampleFixture(t *testing.T) {
	validator, err := NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := loadYAMLAsAny(t, filepath.Join(fixturesDir(t), "web-app-builder.bundle.yaml"))
	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected the bundle fixture to pass, got errors: %+v", errs)
	}
}

func TestBundleValidator_FlowNeedsNoOrchestrationBlock(t *testing.T) {
	validator, err := NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := map[string]any{
		"bundle": "flow-bundle", "version": "1.0", "type": "flow",
		"agents": []any{
			map[string]any{"ref": "a"},
			map[string]any{"ref": "b"},
		},
	}
	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("a flow Bundle without an orchestration block should pass, got errors: %+v", errs)
	}
}

func TestBundleValidator_SingleRejectsMoreThanOneAgent(t *testing.T) {
	validator, err := NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := map[string]any{
		"bundle": "solo", "version": "1.0", "type": "single",
		"agents": []any{
			map[string]any{"ref": "a"},
			map[string]any{"ref": "b"},
		},
	}
	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("a single Bundle with two agents should be rejected")
	}
}

func TestBundleValidator_GraphStillRequiresOrchestration(t *testing.T) {
	validator, err := NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	doc := map[string]any{
		"bundle": "no-orch", "version": "1.0",
		"agents": []any{map[string]any{"ref": "a"}},
	}
	errs, err := validator.Validate(doc)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("a graph Bundle (the default when type is omitted) without orchestration should be rejected")
	}
}

func TestValidateJSON_MalformedJSONReturnsFieldError(t *testing.T) {
	validator, err := NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errs, err := validator.ValidateJSON([]byte("{not valid json"))
	if err != nil {
		t.Fatalf("ValidateJSON should not itself error on bad JSON: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a field error for malformed JSON")
	}
}
