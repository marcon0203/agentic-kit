package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeKindStore is an in-memory resourceKindStore for handler tests — the
// real Postgres-backed implementations in resource_kind.go are verified
// against a live database separately (unique-ref-per-owner constraint,
// JSONB containment query for delete-check, in particular).
type fakeKindStore struct {
	rows       map[int64]resourceRow
	refTaken   map[string]bool
	nextID     int64
	references map[string][]agentReference
}

func newFakeKindStore() *fakeKindStore {
	return &fakeKindStore{
		rows:       map[int64]resourceRow{},
		refTaken:   map[string]bool{},
		nextID:     1,
		references: map[string][]agentReference{},
	}
}

func (s *fakeKindStore) Create(_ context.Context, ownerID int64, ref, version string, config, displayMeta []byte) (resourceRow, error) {
	if s.refTaken[ref] {
		// A real *pgconn.PgError, not a stand-in — isUniqueViolation must
		// exercise the same errors.As path it does against production
		// Postgres (already verified live for the identical pattern in
		// auth_user_store.go).
		return resourceRow{}, &pgconn.PgError{Code: "23505"}
	}
	s.refTaken[ref] = true
	row := resourceRow{ID: s.nextID, OwnerUserID: ownerID, Ref: ref, Version: version, Config: config, DisplayMeta: displayMeta, Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}}
	s.rows[s.nextID] = row
	s.nextID++
	return row, nil
}

func (s *fakeKindStore) GetByID(_ context.Context, id, ownerID int64) (resourceRow, error) {
	row, ok := s.rows[id]
	if !ok || row.OwnerUserID != ownerID {
		return resourceRow{}, errNotFoundForTest
	}
	return row, nil
}

func (s *fakeKindStore) ListPage(_ context.Context, ownerID, afterID int64, limit int32) ([]resourceRow, error) {
	var out []resourceRow
	for id := afterID + 1; id < s.nextID && int32(len(out)) < limit; id++ {
		if row, ok := s.rows[id]; ok && row.OwnerUserID == ownerID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *fakeKindStore) Update(_ context.Context, id, ownerID int64, displayMeta, config []byte, status int16) (resourceRow, error) {
	row, ok := s.rows[id]
	if !ok || row.OwnerUserID != ownerID {
		return resourceRow{}, errNotFoundForTest
	}
	row.DisplayMeta = displayMeta
	row.Config = config
	row.Status = status
	s.rows[id] = row
	return row, nil
}

func (s *fakeKindStore) FindReferencingAgents(_ context.Context, _ int64, ref string) ([]agentReference, error) {
	return s.references[ref], nil
}

var errNotFoundForTest = errors.New("not found")

type noopMCPChecker struct{}

func (noopMCPChecker) Check(context.Context, map[string]any) string { return "healthy" }

func newResourceHandlersForTest() (*ResourceHandlers, *fakeKindStore) {
	tools := newFakeKindStore()
	h := &ResourceHandlers{
		Kinds: map[ResourceKind]resourceKindStore{
			ResourceKindTool:          tools,
			ResourceKindSkill:         newFakeKindStore(),
			ResourceKindMCP:           newFakeKindStore(),
			ResourceKindKnowledgeBase: newFakeKindStore(),
		},
		MCP:    noopMCPChecker{},
		AESKey: testAESKey(),
	}
	return h, tools
}

func doResourceRequest(t *testing.T, handler http.HandlerFunc, method, path string, userID int64, body any, urlParams map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r = r.WithContext(WithUserID(r.Context(), userID))

	if len(urlParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range urlParams {
			rctx.URLParams.Add(k, v)
		}
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestCreateResource_Success(t *testing.T) {
	h, _ := newResourceHandlersForTest()

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "internal-search", DisplayName: "Internal Search",
		Config: map[string]any{"endpoint": "https://mcp.internal/search"},
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var dto resourceDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if dto.ID != "tool_1" {
		t.Fatalf("id = %q, want tool_1", dto.ID)
	}
	if dto.DisplayName != "Internal Search" {
		t.Fatalf("display_name = %q, want Internal Search", dto.DisplayName)
	}
}

func TestCreateResource_DuplicateRefReturns409(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	req := createResourceRequest{Type: ResourceKindTool, Ref: "dup-tool", Config: map[string]any{"endpoint": "https://x"}}

	w1 := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, req, nil)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want 201: %s", w1.Code, w1.Body.String())
	}

	w2 := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, req, nil)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409: %s", w2.Code, w2.Body.String())
	}
	if !containsCode(w2.Body.String(), ErrResourceRefDuplicate) {
		t.Fatalf("body should carry ErrResourceRefDuplicate: %s", w2.Body.String())
	}
}

func TestCreateResource_InvalidRefReturns400WithDetails(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "Not-Valid!", Config: map[string]any{"endpoint": "https://x"},
	}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Details) == 0 {
		t.Fatal("expected field-level details for an invalid ref")
	}
}

func TestCreateResource_CredentialNeverInResponse(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindMCP, Ref: "secure-mcp",
		Config: map[string]any{"endpoint": "https://mcp.internal", "api_key": "sk-should-never-appear"},
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-should-never-appear")) {
		t.Fatalf("credential leaked into create response: %s", w.Body.String())
	}
}

func TestUpdateResource_EnablesDisables(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	create := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "toggle-me", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	var env Envelope
	_ = json.Unmarshal(create.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var created resourceDTO
	_ = json.Unmarshal(dataBytes, &created)

	disabled := int16(2)
	w := doResourceRequest(t, h.Update, http.MethodPatch, "/api/v1/resources/"+created.ID, 1,
		updateResourceRequest{Status: &disabled}, map[string]string{"id": created.ID})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var updatedEnv Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &updatedEnv)
	updatedBytes, _ := json.Marshal(updatedEnv.Data)
	var updated resourceDTO
	_ = json.Unmarshal(updatedBytes, &updated)
	if updated.Status != 2 {
		t.Fatalf("status = %d, want 2 (disabled)", updated.Status)
	}
}

func TestUpdateResource_WrongOwnerReturns404(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	create := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "owner-only", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	var env Envelope
	_ = json.Unmarshal(create.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var created resourceDTO
	_ = json.Unmarshal(dataBytes, &created)

	w := doResourceRequest(t, h.Update, http.MethodPatch, "/api/v1/resources/"+created.ID, 999,
		updateResourceRequest{}, map[string]string{"id": created.ID})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (resource scoped to a different owner)", w.Code)
	}
}

func TestDeleteCheck_DeletableWhenUnreferenced(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	create := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "unreferenced", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	var env Envelope
	_ = json.Unmarshal(create.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var created resourceDTO
	_ = json.Unmarshal(dataBytes, &created)

	w := doResourceRequest(t, h.DeleteCheck, http.MethodGet, "/api/v1/resources/"+created.ID+"/delete-check", 1,
		nil, map[string]string{"id": created.ID})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var checkEnv Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &checkEnv)
	checkBytes, _ := json.Marshal(checkEnv.Data)
	var check deleteCheckDTO
	_ = json.Unmarshal(checkBytes, &check)
	if !check.Deletable {
		t.Fatalf("expected deletable=true for an unreferenced resource: %+v", check)
	}
}

func TestDeleteCheck_NotDeletableWhenReferenced(t *testing.T) {
	h, tools := newResourceHandlersForTest()
	create := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "referenced-tool", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	var env Envelope
	_ = json.Unmarshal(create.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var created resourceDTO
	_ = json.Unmarshal(dataBytes, &created)

	tools.references["referenced-tool"] = []agentReference{{OwnerUserID: 1, AgentRef: "architect", Version: "1.0.0"}}

	w := doResourceRequest(t, h.DeleteCheck, http.MethodGet, "/api/v1/resources/"+created.ID+"/delete-check", 1,
		nil, map[string]string{"id": created.ID})

	var checkEnv Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &checkEnv)
	checkBytes, _ := json.Marshal(checkEnv.Data)
	var check deleteCheckDTO
	_ = json.Unmarshal(checkBytes, &check)
	if check.Deletable {
		t.Fatal("expected deletable=false when an agent references the resource")
	}
	if len(check.ReferencedBy) != 1 || check.ReferencedBy[0].Ref != "architect" {
		t.Fatalf("unexpected referenced_by: %+v", check.ReferencedBy)
	}
}

func TestListResources_FiltersByType(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindTool, Ref: "t1", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: ResourceKindSkill, Ref: "s1", Config: map[string]any{"x": "y"},
	}, nil)

	w := doResourceRequest(t, h.List, http.MethodGet, "/api/v1/resources?type=tool", 1, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var page struct {
		Items []resourceDTO `json:"items"`
	}
	_ = json.Unmarshal(dataBytes, &page)
	if len(page.Items) != 1 || page.Items[0].Type != ResourceKindTool {
		t.Fatalf("expected exactly 1 tool, got %+v", page.Items)
	}
}
