package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/dslschema"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// fakeAgentQuerier embeds the (nil) store.Querier interface and overrides
// only the methods agent_handlers.go and resource_ref_check.go actually
// call — any other method panics on a nil embedded interface, which is
// fine: these tests never exercise those paths. The real *store.Queries
// implementation is exercised by the live-Postgres pass below.
type fakeAgentQuerier struct {
	store.Querier

	agents             map[string][]store.Agent // agent_ref -> versions, newest first
	nextID             int64
	toolStatus         map[string]int16 // ref -> status (1 enabled, 2 disabled); absent = not found
	skillStatus        map[string]int16
	subscribedCounts   map[string]int64
	referencingBundles map[string][]store.FindBundlesReferencingAgentRefRow
}

func newFakeAgentQuerier() *fakeAgentQuerier {
	return &fakeAgentQuerier{
		agents:             map[string][]store.Agent{},
		nextID:             1,
		toolStatus:         map[string]int16{},
		skillStatus:        map[string]int16{},
		subscribedCounts:   map[string]int64{},
		referencingBundles: map[string][]store.FindBundlesReferencingAgentRefRow{},
	}
}

func (f *fakeAgentQuerier) CreateAgent(_ context.Context, arg store.CreateAgentParams) (store.Agent, error) {
	for _, v := range f.agents[arg.AgentRef] {
		if v.Version == arg.Version {
			// A real *pgconn.PgError so isUniqueViolation's errors.As path
			// is exercised the same way it is against production Postgres.
			return store.Agent{}, &pgconn.PgError{Code: "23505"}
		}
	}
	row := store.Agent{
		ID: f.nextID, OwnerUserID: arg.OwnerUserID, AgentRef: arg.AgentRef, Version: arg.Version,
		Definition: arg.Definition, DisplayMeta: arg.DisplayMeta, Status: 1,
		CreatedAt: pgtype.Timestamptz{Valid: true},
	}
	f.nextID++
	f.agents[arg.AgentRef] = append([]store.Agent{row}, f.agents[arg.AgentRef]...)
	return row, nil
}

func (f *fakeAgentQuerier) ListAgentsForOwnerLatestPage(_ context.Context, arg store.ListAgentsForOwnerLatestPageParams) ([]store.Agent, error) {
	var out []store.Agent
	for ref, versions := range f.agents {
		if ref > arg.AgentRef && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if int32(len(out)) > arg.Limit {
		out = out[:arg.Limit]
	}
	return out, nil
}

func (f *fakeAgentQuerier) ListAgentVersionsForOwner(_ context.Context, arg store.ListAgentVersionsForOwnerParams) ([]store.Agent, error) {
	return f.agents[arg.AgentRef], nil
}

func (f *fakeAgentQuerier) CountActiveSubscribedListingsForAgentRef(_ context.Context, arg store.CountActiveSubscribedListingsForAgentRefParams) (int64, error) {
	return f.subscribedCounts[arg.AgentRef], nil
}

func (f *fakeAgentQuerier) FindBundlesReferencingAgentRef(_ context.Context, arg store.FindBundlesReferencingAgentRefParams) ([]store.FindBundlesReferencingAgentRefRow, error) {
	return f.referencingBundles[arg.Column2], nil
}

func (f *fakeAgentQuerier) DeleteAgentsByRef(_ context.Context, arg store.DeleteAgentsByRefParams) error {
	delete(f.agents, arg.AgentRef)
	return nil
}

func (f *fakeAgentQuerier) GetToolLatestStatusByRef(_ context.Context, arg store.GetToolLatestStatusByRefParams) (int16, error) {
	return lookupStatus(f.toolStatus, arg.Ref)
}
func (f *fakeAgentQuerier) GetMCPServerLatestStatusByRef(_ context.Context, arg store.GetMCPServerLatestStatusByRefParams) (int16, error) {
	return lookupStatus(f.toolStatus, arg.Ref)
}
func (f *fakeAgentQuerier) GetKnowledgeBaseLatestStatusByRef(_ context.Context, arg store.GetKnowledgeBaseLatestStatusByRefParams) (int16, error) {
	return lookupStatus(f.toolStatus, arg.Ref)
}
func (f *fakeAgentQuerier) GetSkillLatestStatusByRef(_ context.Context, arg store.GetSkillLatestStatusByRefParams) (int16, error) {
	return lookupStatus(f.skillStatus, arg.Ref)
}

func lookupStatus(m map[string]int16, ref string) (int16, error) {
	status, ok := m[ref]
	if !ok {
		return 0, pgx.ErrNoRows
	}
	return status, nil
}

func newAgentHandlersForTest(t *testing.T) (*AgentHandlers, *fakeAgentQuerier) {
	t.Helper()
	validator, err := dslschema.NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	q := newFakeAgentQuerier()
	h := NewAgentHandlers(q, validator, NewResourceRefChecker(q))
	return h, q
}

func validAgentDefinition(agentRef, version string) map[string]any {
	return map[string]any{
		"agent":   agentRef,
		"role":    "Test Role",
		"version": version,
		"model":   map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona": "You are a test agent.",
		"capabilities": map[string]any{
			"tools":  []any{},
			"skills": []any{},
		},
		"constraints": map[string]any{},
	}
}

func TestCreateAgent_Success(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{
		Definition: validAgentDefinition("architect", "1.0"),
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_InvalidProviderReturns400With40001(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)

	def := validAgentDefinition("architect", "1.0")
	def["model"].(map[string]any)["provider"] = "not-a-provider"

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrAgentSchemaInvalid) {
		t.Fatalf("body should carry ErrAgentSchemaInvalid (40001): %s", w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Details) == 0 {
		t.Fatal("expected field-level details for an invalid provider")
	}
}

func TestCreateAgent_DisabledResourceReturns30002(t *testing.T) {
	h, q := newAgentHandlersForTest(t)
	q.toolStatus["broken-tool"] = 2 // disabled

	def := validAgentDefinition("architect", "1.0")
	def["capabilities"].(map[string]any)["tools"] = []any{"broken-tool"}

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrResourceDisabled) {
		t.Fatalf("body should carry ErrResourceDisabled (30002): %s", w.Body.String())
	}
}

func TestCreateAgent_NonexistentResourceReturns30002(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)

	def := validAgentDefinition("architect", "1.0")
	def["capabilities"].(map[string]any)["tools"] = []any{"does-not-exist"}

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrResourceDisabled) {
		t.Fatalf("body should carry ErrResourceDisabled (30002): %s", w.Body.String())
	}
}

func TestCreateAgent_EnabledResourcePasses(t *testing.T) {
	h, q := newAgentHandlersForTest(t)
	q.toolStatus["good-tool"] = 1
	q.skillStatus["good-skill"] = 1

	def := validAgentDefinition("architect", "1.0")
	def["capabilities"].(map[string]any)["tools"] = []any{"good-tool"}
	def["capabilities"].(map[string]any)["skills"] = []any{"good-skill"}

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_DuplicateVersionReturns409(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	def := validAgentDefinition("dup_agent", "1.0")

	w1 := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want 201: %s", w1.Code, w1.Body.String())
	}

	w2 := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409: %s", w2.Code, w2.Body.String())
	}
	if !containsCode(w2.Body.String(), ErrResourceRefDuplicate) {
		t.Fatalf("body should carry ErrResourceRefDuplicate: %s", w2.Body.String())
	}
}

func TestListAgentVersions_ReturnsDescendingByCreation(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: validAgentDefinition("pm", "1.0")}, nil)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: validAgentDefinition("pm", "2.0")}, nil)

	w := doResourceRequest(t, h.ListVersions, http.MethodGet, "/api/v1/agents/pm/versions", 1, nil, map[string]string{"ref": "pm"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var versions []agentDTO
	_ = json.Unmarshal(dataBytes, &versions)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != "2.0" {
		t.Fatalf("expected newest version first, got %q", versions[0].Version)
	}
}

func TestListAgentVersions_UnknownRefReturns404(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	w := doResourceRequest(t, h.ListVersions, http.MethodGet, "/api/v1/agents/nope/versions", 1, nil, map[string]string{"ref": "nope"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestDeleteAgent_BlockedBySubscribedListing(t *testing.T) {
	h, q := newAgentHandlersForTest(t)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: validAgentDefinition("subscribed-agent", "1.0")}, nil)
	q.subscribedCounts["subscribed-agent"] = 1

	w := doAgentDelete(t, h.Delete, "subscribed-agent")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrSubscribedVersionLocked) {
		t.Fatalf("body should carry ErrSubscribedVersionLocked (70005): %s", w.Body.String())
	}
}

func TestDeleteAgent_BlockedByBundleReference(t *testing.T) {
	h, q := newAgentHandlersForTest(t)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: validAgentDefinition("used-agent", "1.0")}, nil)
	q.referencingBundles["used-agent"] = []store.FindBundlesReferencingAgentRefRow{{BundleRef: "web-app-builder", Version: "2.0"}}

	w := doAgentDelete(t, h.Delete, "used-agent")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrAgentVersionNotFound) {
		t.Fatalf("body should carry ErrAgentVersionNotFound (40004): %s", w.Body.String())
	}
}

func TestDeleteAgent_SucceedsWhenUnoccupied(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: validAgentDefinition("free-agent", "1.0")}, nil)

	w := doAgentDelete(t, h.Delete, "free-agent")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
}

func doAgentDelete(t *testing.T, handler http.HandlerFunc, ref string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+ref, &bytes.Buffer{})
	r = r.WithContext(WithUserID(r.Context(), 1))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ref", ref)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
