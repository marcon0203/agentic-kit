package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"gopkg.in/yaml.v3"

	"github.com/marcon0203/agentic-kit/internal/dslschema"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// fakeBundleQuerier is the Bundle-handler counterpart to fakeAgentQuerier:
// embeds the nil store.Querier interface and overrides only what
// bundle_handlers.go and bundle_handoff_check.go call.
type fakeBundleQuerier struct {
	store.Querier

	bundles          map[string][]store.Bundle
	nextID           int64
	subscribedCounts map[string]int64
	agentsByRef      map[string]store.Agent
}

func newFakeBundleQuerier() *fakeBundleQuerier {
	return &fakeBundleQuerier{
		bundles:          map[string][]store.Bundle{},
		nextID:           1,
		subscribedCounts: map[string]int64{},
		agentsByRef:      map[string]store.Agent{},
	}
}

func (f *fakeBundleQuerier) CreateBundle(_ context.Context, arg store.CreateBundleParams) (store.Bundle, error) {
	for _, v := range f.bundles[arg.BundleRef] {
		if v.Version == arg.Version {
			return store.Bundle{}, &pgconn.PgError{Code: "23505"}
		}
	}
	row := store.Bundle{
		ID: f.nextID, OwnerUserID: arg.OwnerUserID, BundleRef: arg.BundleRef, Version: arg.Version,
		Definition: arg.Definition, DisplayMeta: arg.DisplayMeta, Status: 1,
		CreatedAt: pgtype.Timestamptz{Valid: true},
	}
	f.nextID++
	f.bundles[arg.BundleRef] = append([]store.Bundle{row}, f.bundles[arg.BundleRef]...)
	return row, nil
}

func (f *fakeBundleQuerier) ListBundlesForOwnerLatestPage(_ context.Context, arg store.ListBundlesForOwnerLatestPageParams) ([]store.Bundle, error) {
	var out []store.Bundle
	for ref, versions := range f.bundles {
		if ref > arg.BundleRef && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if int32(len(out)) > arg.Limit {
		out = out[:arg.Limit]
	}
	return out, nil
}

func (f *fakeBundleQuerier) CountActiveSubscribedListingsForBundleRef(_ context.Context, arg store.CountActiveSubscribedListingsForBundleRefParams) (int64, error) {
	return f.subscribedCounts[arg.BundleRef], nil
}

func (f *fakeBundleQuerier) DeleteBundlesByRef(_ context.Context, arg store.DeleteBundlesByRefParams) error {
	delete(f.bundles, arg.BundleRef)
	return nil
}

func (f *fakeBundleQuerier) GetAgentLatestByRef(_ context.Context, arg store.GetAgentLatestByRefParams) (store.Agent, error) {
	a, ok := f.agentsByRef[arg.AgentRef]
	if !ok {
		return store.Agent{}, pgx.ErrNoRows
	}
	return a, nil
}

func newBundleHandlersForTest(t *testing.T) (*BundleHandlers, *fakeBundleQuerier) {
	t.Helper()
	validator, err := dslschema.NewBundleValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	q := newFakeBundleQuerier()
	return NewBundleHandlers(q, validator), q
}

// validBundleDefinition mirrors the shape of
// schemas/examples/web-app-builder.bundle.yaml closely enough to pass
// schema validation, with a bundle ref the caller controls.
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

func doBundleRequest(t *testing.T, handler http.HandlerFunc, method, path string, userID int64, body any, urlParams map[string]string) *httptest.ResponseRecorder {
	return doResourceRequest(t, handler, method, path, userID, body, urlParams)
}

func TestCreateBundle_Success(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{
		Definition: validBundleDefinition("test-bundle"),
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestCreateBundle_WebAppBuilderFixturePasses(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	def := loadBundleFixture(t, "web-app-builder.bundle.yaml")

	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// loadBundleFixture reads a schemas/examples/*.yaml fixture and normalizes
// it into the map[string]any / []any / string shape json.Decode would
// produce — the same shape dslschema.Validator.Validate and
// bundlegraph.ParseGraph expect throughout, matching
// internal/dslschema's own loadYAMLAsAny helper.
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

// normalizeYAMLMap converts yaml.v3's already-string-keyed maps deeply,
// matching what json.Decode would produce from the equivalent JSON —
// needed because dslschema.Validator.Validate walks the tree expecting
// that shape throughout (nested maps/slices), same as loadYAMLAsAny does
// for internal/dslschema's own tests.
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

func TestCreateBundle_MissingEntryReturns422With40003(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	def := validBundleDefinition("bad-entry-bundle")
	def["orchestration"].(map[string]any)["entry"] = "not-a-declared-agent"

	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrBundleGraphInvalid) {
		t.Fatalf("body should carry ErrBundleGraphInvalid (40003): %s", w.Body.String())
	}
}

func TestCreateBundle_SelfLoopOnlyDeadlockReturns422(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	def := map[string]any{
		"bundle":  "deadlocked-bundle",
		"version": "1.0",
		"agents": []any{
			map[string]any{"ref": "a"},
			map[string]any{"ref": "b"},
		},
		"orchestration": map[string]any{
			"mode":  "graph",
			"entry": "a",
			"edges": []any{
				map[string]any{"from": "a", "to": "END"},
				map[string]any{"from": "b", "to": "b"}, // b's only incoming edge is itself
			},
		},
	}

	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
}

func TestCreateBundle_RetrySelfLoopDoesNotBlockSave(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	def := map[string]any{
		"bundle":  "retry-bundle",
		"version": "1.0",
		"agents": []any{
			map[string]any{"ref": "a"},
			map[string]any{"ref": "b"},
		},
		"orchestration": map[string]any{
			"mode":  "graph",
			"entry": "a",
			"edges": []any{
				map[string]any{"from": "a", "to": "b"},
				map[string]any{"from": "b", "to": "END", "condition": "shared_state.ok == true"},
				map[string]any{"from": "b", "to": "b", "condition": "shared_state.ok == false"},
			},
		},
	}

	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (legit retry self-loop must not block save): %s", w.Code, w.Body.String())
	}
}

func TestCreateBundle_DuplicateVersionReturns409(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	def := validBundleDefinition("dup-bundle")

	w1 := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want 201: %s", w1.Code, w1.Body.String())
	}
	w2 := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateBundle_HandoffMismatchProducesWarningNotError(t *testing.T) {
	h, q := newBundleHandlersForTest(t)

	architectDef := map[string]any{
		"handoff": map[string]any{
			"accepts_input_from": []any{"someone_else"}, // does NOT include product_manager
		},
	}
	architectDefBytes, _ := json.Marshal(architectDef)
	q.agentsByRef["architect"] = store.Agent{ID: 1, AgentRef: "architect", Definition: architectDefBytes}

	def := validBundleDefinition("handoff-mismatch-bundle")
	w := doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: def}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (handoff mismatch is a warning, not blocking): %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "warnings") {
		t.Fatalf("expected a warnings field surfacing the handoff mismatch: %s", w.Body.String())
	}
}

func TestDeleteBundle_BlockedBySubscribedListing(t *testing.T) {
	h, q := newBundleHandlersForTest(t)
	doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: validBundleDefinition("subscribed-bundle")}, nil)
	q.subscribedCounts["subscribed-bundle"] = 2

	w := doBundleDelete(t, h.Delete, "subscribed-bundle")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrSubscribedVersionLocked) {
		t.Fatalf("body should carry ErrSubscribedVersionLocked (70005): %s", w.Body.String())
	}
}

func TestDeleteBundle_SucceedsWhenUnoccupied(t *testing.T) {
	h, _ := newBundleHandlersForTest(t)
	doBundleRequest(t, h.Create, http.MethodPost, "/api/v1/bundles", 1, createBundleRequest{Definition: validBundleDefinition("free-bundle")}, nil)

	w := doBundleDelete(t, h.Delete, "free-bundle")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
}

func doBundleDelete(t *testing.T, handler http.HandlerFunc, ref string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/bundles/"+ref, nil)
	r = r.WithContext(WithUserID(r.Context(), 1))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ref", ref)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
