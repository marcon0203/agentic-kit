package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	adapterschema "github.com/marcon0203/agentic-kit/internal/adapter/schema"
	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
)

// Graph rules (blocking vs warning), handoff drift and delete occupancy are
// covered by internal/domain/bundle's own tests. Here: the real JSON Schema
// and real graph validator wired end to end against a shipped fixture, the
// Kind->status mapping, and the warnings field's wire shape.

type stubBundleRepo struct {
	bundles    map[string][]bundle.Bundle
	nextID     int64
	subscribed map[string]int64
}

func newStubBundleRepo() *stubBundleRepo {
	return &stubBundleRepo{bundles: map[string][]bundle.Bundle{}, nextID: 1, subscribed: map[string]int64{}}
}

func (s *stubBundleRepo) ListLatestByOwner(_ context.Context, _ int64, q domain.PageQuery) ([]bundle.Bundle, error) {
	var out []bundle.Bundle
	for ref, versions := range s.bundles {
		if ref > q.After && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (s *stubBundleRepo) Create(_ context.Context, b bundle.Bundle) (bundle.Bundle, error) {
	for _, v := range s.bundles[b.Ref] {
		if v.Version == b.Version {
			return bundle.Bundle{}, bundle.ErrDuplicateVersion
		}
	}
	b.ID, b.Status = s.nextID, bundle.StatusEnabled
	s.nextID++
	s.bundles[b.Ref] = append([]bundle.Bundle{b}, s.bundles[b.Ref]...)
	return b, nil
}

func (s *stubBundleRepo) DeleteByRef(_ context.Context, _ int64, ref string) error {
	delete(s.bundles, ref)
	return nil
}

func (s *stubBundleRepo) CountActiveSubscribedVersions(_ context.Context, _ int64, ref string) (int64, error) {
	return s.subscribed[ref], nil
}

type stubHandoffs struct{ byRef map[string]bundle.Handoff }

func (s stubHandoffs) Lookup(_ context.Context, _ int64, ref string) (bundle.Handoff, error) {
	return s.byRef[ref], nil
}

func newBundleHandlersForTest(t *testing.T) (*BundleHandlers, *stubBundleRepo, stubHandoffs) {
	t.Helper()
	validator, err := dslschema.NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	repo := newStubBundleRepo()
	handoffs := stubHandoffs{byRef: map[string]bundle.Handoff{}}
	svc := bundle.NewService(repo, handoffs, adapterschema.NewValidator(validator))
	return NewBundleHandlers(svc), repo, handoffs
}

func validBundleDefinition(bundleRef string) map[string]any {
	return map[string]any{
		"bundle":  bundleRef,
		"version": "1.0",
		"agents": []any{
			map[string]any{"ref": "product_manager"},
			map[string]any{"ref": "architect"},
		},
		"orchestration": map[string]any{
			"mode":  "graph",
			"entry": "product_manager",
			"edges": []any{
				map[string]any{"from": "product_manager", "to": "architect"},
				map[string]any{"from": "architect", "to": "END"},
			},
		},
	}
}

func TestCreateBundle_Success201(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{
		Definition: validBundleDefinition("test-bundle"),
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	// Without warnings the response keeps the plain Bundle shape.
	if strings.Contains(w.Body.String(), "warnings") {
		t.Fatalf("a clean bundle must not carry a warnings field: %s", w.Body.String())
	}
}

// The shipped example must survive the real schema and the real graph
// validator — this is what catches a spec/fixture drift.
func TestCreateBundle_ShippedFixturePasses(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)
	def := loadBundleFixture(t, "web-app-builder.bundle.yaml")

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// domain.KindUnprocessable -> 422, carrying 40003.
func TestCreateBundle_BlockingGraphIssueMapsTo422(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)
	def := validBundleDefinition("bad-entry-bundle")
	def["orchestration"].(map[string]any)["entry"] = "not-a-declared-agent"

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrBundleGraphInvalid) {
		t.Fatalf("body should carry ErrBundleGraphInvalid (40003): %s", w.Body.String())
	}
}

// The real JSON Schema, not a stub — proves the adapter is wired in.
func TestCreateBundle_RealSchemaRejectsBadModeAs400(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)
	def := validBundleDefinition("bad-mode-bundle")
	def["orchestration"].(map[string]any)["mode"] = "not-a-mode"

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrBundleSchemaInvalid) {
		t.Fatalf("body should carry ErrBundleSchemaInvalid (40002): %s", w.Body.String())
	}
}

func TestCreateBundle_DuplicateVersionMapsTo409(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)
	def := validBundleDefinition("dup-bundle")

	if w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil); w.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want 201: %s", w.Code, w.Body.String())
	}
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409: %s", w.Code, w.Body.String())
	}
}

// Warnings must ride along on a 201, flattened into the Bundle object rather
// than nested under it.
func TestCreateBundle_WarningsAppearOnA201(t *testing.T) {
	h, _, handoffs := newBundleHandlersForTest(t)
	handoffs.byRef["architect"] = bundle.Handoff{AcceptsInputFrom: []string{"someone_else"}}

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{
		Definition: validBundleDefinition("handoff-mismatch-bundle"),
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (drift is a warning, not blocking): %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	data, _ := json.Marshal(env.Data)
	var resp struct {
		BundleRef string       `json:"bundle_ref"`
		Warnings  []FieldError `json:"warnings"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if resp.BundleRef != "handoff-mismatch-bundle" {
		t.Fatalf("the Bundle fields must stay flat alongside warnings, got %q", resp.BundleRef)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("expected one drift warning on the wire, got %+v", resp.Warnings)
	}
}

func TestDeleteBundle_OccupiedMapsTo409(t *testing.T) {
	h, repo, _ := newBundleHandlersForTest(t)
	repo.subscribed["subscribed-bundle"] = 1

	w := doResourceRequest(t, h.Delete, http.MethodDelete, "/api/v1/bundles/subscribed-bundle", 1, nil,
		map[string]string{"ref": "subscribed-bundle"})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrSubscribedVersionLocked) {
		t.Fatalf("body should carry 70005: %s", w.Body.String())
	}
}

func TestDeleteBundle_Succeeds204(t *testing.T) {
	h, _, _ := newBundleHandlersForTest(t)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{
		Definition: validBundleDefinition("free-bundle"),
	}, nil)

	w := doResourceRequest(t, h.Delete, http.MethodDelete, "/api/v1/bundles/free-bundle", 1, nil,
		map[string]string{"ref": "free-bundle"})

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
}

// loadBundleFixture reads a schemas/examples/*.yaml fixture and normalizes it
// into the map[string]any / []any shape json.Decode would produce — the shape
// the schema validator and graph parser expect throughout.
func loadBundleFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	dir, err := filepath.Abs("../../schemas/examples")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return normalizeYAMLMap(doc)
}

func normalizeYAMLMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = normalizeYAMLValue(v)
	}
	return out
}

func normalizeYAMLValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return normalizeYAMLMap(val)
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeYAMLValue(vv)
		}
		return out
	default:
		return val
	}
}
