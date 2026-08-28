package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	adapterschema "github.com/marcon0203/agentic-kit/internal/adapter/schema"
	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
)

// The Agent business rules are covered by internal/domain/agent's own tests,
// against in-memory fakes and with no HTTP involved. What is left to prove
// *here* is the transport contract: that a domain error Kind becomes the
// right HTTP status, that its business code and details survive into the
// envelope, and that the real JSON Schema validator is actually wired in.

type stubAgentRepo struct {
	agents      map[string][]agent.Agent
	nextID      int64
	subscribed  map[string]int64
	referencing map[string][]agent.BundleRef
}

func newStubAgentRepo() *stubAgentRepo {
	return &stubAgentRepo{
		agents: map[string][]agent.Agent{}, nextID: 1,
		subscribed: map[string]int64{}, referencing: map[string][]agent.BundleRef{},
	}
}

func (s *stubAgentRepo) ListLatestByOwner(_ context.Context, _ int64, q domain.PageQuery) ([]agent.Agent, error) {
	var out []agent.Agent
	for ref, versions := range s.agents {
		if ref > q.After && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (s *stubAgentRepo) ListVersions(_ context.Context, _ int64, ref string) ([]agent.Agent, error) {
	return s.agents[ref], nil
}

func (s *stubAgentRepo) Create(_ context.Context, a agent.Agent) (agent.Agent, error) {
	for _, v := range s.agents[a.Ref] {
		if v.Version == a.Version {
			return agent.Agent{}, agent.ErrDuplicateVersion
		}
	}
	a.ID, a.Status = s.nextID, agent.StatusEnabled
	s.nextID++
	s.agents[a.Ref] = append([]agent.Agent{a}, s.agents[a.Ref]...)
	return a, nil
}

func (s *stubAgentRepo) DeleteByRef(_ context.Context, _ int64, ref string) error {
	delete(s.agents, ref)
	return nil
}

func (s *stubAgentRepo) CountActiveSubscribedVersions(_ context.Context, _ int64, ref string) (int64, error) {
	return s.subscribed[ref], nil
}

func (s *stubAgentRepo) FindReferencingBundles(_ context.Context, _ int64, ref string) ([]agent.BundleRef, error) {
	return s.referencing[ref], nil
}

func (s *stubAgentRepo) GetByID(_ context.Context, _ int64, id int64) (agent.Agent, error) {
	for _, versions := range s.agents {
		for _, v := range versions {
			if v.ID == id {
				return v, nil
			}
		}
	}
	return agent.Agent{}, errors.New("not found")
}

func (s *stubAgentRepo) seed(ref, version string) int64 {
	a := agent.Agent{ID: s.nextID, Ref: ref, Version: version, Status: agent.StatusEnabled,
		Definition: agent.Definition(validAgentDefinition(ref, version))}
	s.nextID++
	s.agents[ref] = append([]agent.Agent{a}, s.agents[ref]...)
	return a.ID
}

// stubCatalog reports every ref as present and enabled — capability
// resolution is a domain rule tested there, not a transport concern.
type stubCatalog struct{}

func (stubCatalog) ToolStatus(context.Context, int64, string) (agent.RefStatus, error) {
	return agent.RefStatus{Found: true, Enabled: true}, nil
}
func (stubCatalog) SkillStatus(context.Context, int64, string) (agent.RefStatus, error) {
	return agent.RefStatus{Found: true, Enabled: true}, nil
}

func newAgentHandlersForTest(t *testing.T) (*AgentHandlers, *stubAgentRepo) {
	t.Helper()
	validator, err := dslschema.NewAgentValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	repo := newStubAgentRepo()
	svc := agent.NewService(repo, stubCatalog{}, adapterschema.NewValidator(validator))
	return NewAgentHandlers(svc), repo
}

func validAgentDefinition(agentRef, version string) map[string]any {
	return map[string]any{
		"agent":        agentRef,
		"role":         "Test Role",
		"version":      version,
		"model":        map[string]any{"provider": "anthropic", "name": "claude-sonnet-5"},
		"persona":      "You are a test agent.",
		"capabilities": map[string]any{"tools": []any{}, "skills": []any{}},
		"constraints":  map[string]any{},
	}
}

func TestCreateAgent_Success201(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{
		Definition: validAgentDefinition("architect", "1.0"),
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// Proves the real schema validator is wired through the adapter into the
// service — a stub validator here would make this test vacuous.
func TestCreateAgent_RealSchemaRejectsBadProviderAsInvalid400(t *testing.T) {
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
		t.Fatal("domain field errors must survive into the envelope's details")
	}
}

// domain.KindConflict -> 409.
func TestCreateAgent_DuplicateVersionMapsToConflict409(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	def := validAgentDefinition("dup_agent", "1.0")

	if w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil); w.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want 201: %s", w.Code, w.Body.String())
	}

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/agents", 1, createAgentRequest{Definition: def}, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrResourceRefDuplicate) {
		t.Fatalf("body should carry ErrResourceRefDuplicate: %s", w.Body.String())
	}
}

// domain.KindNotFound -> 404.
func TestListAgentVersions_UnknownIDMapsToNotFound404(t *testing.T) {
	h, _ := newAgentHandlersForTest(t)
	w := doResourceRequest(t, h.ListVersions, http.MethodGet, "/api/v1/agents/999/versions", 1, nil, map[string]string{"id": "999"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestListAgentVersions_SerializesNewestFirst(t *testing.T) {
	h, repo := newAgentHandlersForTest(t)
	firstID := repo.seed("pm", "1.0")
	repo.seed("pm", "2.0")

	w := doResourceRequest(t, h.ListVersions, http.MethodGet, "/api/v1/agents/"+strconv.FormatInt(firstID, 10)+"/versions", 1, nil, map[string]string{"id": strconv.FormatInt(firstID, 10)})
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
	if versions[0].Definition == nil {
		t.Fatal("the DTO must carry the definition")
	}
}

func TestDeleteAgent_OccupiedMapsToConflict409(t *testing.T) {
	h, repo := newAgentHandlersForTest(t)
	repo.subscribed["subscribed-agent"] = 1
	id := repo.seed("subscribed-agent", "1.0")

	w := doAgentDelete(t, h.Delete, id)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrSubscribedVersionLocked) {
		t.Fatalf("body should carry ErrSubscribedVersionLocked (70005): %s", w.Body.String())
	}
}

func TestDeleteAgent_SucceedsWith204(t *testing.T) {
	h, repo := newAgentHandlersForTest(t)
	id := repo.seed("free-agent", "1.0")

	w := doAgentDelete(t, h.Delete, id)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
}

func doAgentDelete(t *testing.T, handler http.HandlerFunc, id int64) *httptest.ResponseRecorder {
	t.Helper()
	idStr := strconv.FormatInt(id, 10)
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+idStr, &bytes.Buffer{})
	r = r.WithContext(WithUserID(r.Context(), 1))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idStr)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
