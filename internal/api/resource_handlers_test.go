package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// The resource rules — which config keys are credentials, that a redacted
// config omits rather than masks them, the MCP probe, ownership scoping —
// are tested against the service in internal/domain/resource. What is left
// here is transport: the opaque external id, the DTO shapes, and the
// status codes produced before the service is ever reached.

type fakeResourceRepo struct {
	rows       map[int64]resource.Resource
	nextID     int64
	references map[string][]resource.AgentReference
}

func newFakeResourceRepo() *fakeResourceRepo {
	return &fakeResourceRepo{rows: map[int64]resource.Resource{}, nextID: 1, references: map[string][]resource.AgentReference{}}
}

func (f *fakeResourceRepo) Create(_ context.Context, r resource.Resource) (resource.Resource, error) {
	for _, existing := range f.rows {
		if existing.OwnerID == r.OwnerID && existing.Kind == r.Kind && existing.Ref == r.Ref {
			return resource.Resource{}, resource.ErrDuplicate
		}
	}
	r.ID = f.nextID
	f.nextID++
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeResourceRepo) GetByID(_ context.Context, kind resource.Kind, id, ownerID int64) (resource.Resource, error) {
	row, ok := f.rows[id]
	if !ok || row.OwnerID != ownerID || row.Kind != kind {
		return resource.Resource{}, resource.ErrNotFound
	}
	return row, nil
}

func (f *fakeResourceRepo) ListPage(_ context.Context, kind resource.Kind, ownerID, afterID int64, limit int32) ([]resource.Resource, error) {
	var out []resource.Resource
	for id := afterID + 1; id < f.nextID && int32(len(out)) < limit; id++ {
		if row, ok := f.rows[id]; ok && row.OwnerID == ownerID && row.Kind == kind {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeResourceRepo) Update(_ context.Context, r resource.Resource) (resource.Resource, error) {
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeResourceRepo) FindReferencingAgents(_ context.Context, _ resource.Kind, _ int64, ref string) ([]resource.AgentReference, error) {
	return f.references[ref], nil
}

func (f *fakeResourceRepo) SetHealth(_ context.Context, id int64, health resource.Health) error {
	row, ok := f.rows[id]
	if !ok {
		return resource.ErrNotFound
	}
	row.Health = health
	f.rows[id] = row
	return nil
}

// passthroughCipher stands in for AES: these tests are about transport, and
// a cipher that changes nothing keeps the assertions about *which* fields
// appear readable.
type passthroughCipher struct{}

func (passthroughCipher) Encrypt(s string) (string, error) { return s, nil }
func (passthroughCipher) Decrypt(s string) (string, error) { return s, nil }

type healthyProbe struct{}

func (healthyProbe) Check(context.Context, resource.Config) resource.Health {
	return resource.HealthHealthy
}

func newResourceHandlersForTest() (*ResourceHandlers, *fakeResourceRepo) {
	repo := newFakeResourceRepo()
	return NewResourceHandlers(resource.NewService(repo, passthroughCipher{}, healthyProbe{})), repo
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

// decodeResourceDTO unwraps the envelope around a single resource.
func decodeResourceDTO(t *testing.T, w *httptest.ResponseRecorder) resourceDTO {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var dto resourceDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return dto
}

func createTestResource(t *testing.T, h *ResourceHandlers, kind, ref string, config map[string]any) resourceDTO {
	t.Helper()
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: kind, Ref: ref, DisplayName: ref, Config: config,
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s: status = %d, want 201: %s", ref, w.Code, w.Body.String())
	}
	return decodeResourceDTO(t, w)
}

func TestResourceID_RoundTripsKindAndID(t *testing.T) {
	external := encodeResourceID(resource.KindKnowledgeBase, 42)
	if external == "knowledge_base:42" {
		t.Fatal("external id should be opaque, not the raw kind:id pair")
	}
	kind, id, err := decodeResourceID(external)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kind != resource.KindKnowledgeBase || id != 42 {
		t.Fatalf("round trip = (%q, %d), want (knowledge_base, 42)", kind, id)
	}
}

func TestResourceID_RejectsHandCraftedIDs(t *testing.T) {
	for _, bad := range []string{"not base64!!", encodeCursorString("nosuchkind:1"), encodeCursorString("tool:abc"), encodeCursorString("tool")} {
		if _, _, err := decodeResourceID(bad); err == nil {
			t.Fatalf("decodeResourceID(%q) should have failed", bad)
		}
	}
}

func TestUpdateResource_UndecodableIDReturns404(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.Update, http.MethodPatch, "/api/v1/resources/garbage", 1,
		updateResourceRequest{}, map[string]string{"id": "!!!not-base64!!!"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestCreateResource_MalformedBodyReturns400(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resources", bytes.NewBufferString("{not json"))
	r = r.WithContext(WithUserID(r.Context(), 1))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestResourceHandlers_RequireAuthenticatedUser(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	for name, handler := range map[string]http.HandlerFunc{
		"list": h.List, "create": h.Create, "update": h.Update, "delete-check": h.DeleteCheck,
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a user: status = %d, want 401", name, w.Code)
		}
	}
}

func TestCreateResource_ResponseShape(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	dto := createTestResource(t, h, string(resource.KindTool), "internal-search", map[string]any{"endpoint": "https://mcp.internal/search"})

	kind, _, err := decodeResourceID(dto.ID)
	if err != nil {
		t.Fatalf("returned id is not decodable: %v", err)
	}
	if kind != resource.KindTool {
		t.Fatalf("id encodes kind %q, want tool", kind)
	}
	if dto.Type != string(resource.KindTool) || dto.DisplayName != "internal-search" {
		t.Fatalf("unexpected dto: %+v", dto)
	}
	if dto.Config["endpoint"] != "https://mcp.internal/search" {
		t.Fatalf("config not echoed back: %+v", dto.Config)
	}
}

func TestCreateResource_CredentialNeverInResponse(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: string(resource.KindMCP), Ref: "secure-mcp",
		Config: map[string]any{"endpoint": "https://mcp.internal", "api_key": "sk-should-never-appear"},
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-should-never-appear")) {
		t.Fatalf("credential leaked into create response: %s", w.Body.String())
	}
}

func TestCreateResource_DuplicateRefReturns409(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	createTestResource(t, h, string(resource.KindTool), "dup-tool", map[string]any{"endpoint": "https://x"})

	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: string(resource.KindTool), Ref: "dup-tool", Config: map[string]any{"endpoint": "https://x"},
	}, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrResourceRefDuplicate) {
		t.Fatalf("body should carry ErrResourceRefDuplicate: %s", w.Body.String())
	}
}

func TestCreateResource_InvalidRefReturns400WithDetails(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.Create, http.MethodPost, "/api/v1/resources", 1, createResourceRequest{
		Type: string(resource.KindTool), Ref: "Not-Valid!", Config: map[string]any{"endpoint": "https://x"},
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

func TestUpdateResource_StatusPatchIsCarriedThrough(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	created := createTestResource(t, h, string(resource.KindTool), "toggle-me", map[string]any{"endpoint": "https://x"})

	disabled := int16(resource.StatusDisabled)
	w := doResourceRequest(t, h.Update, http.MethodPatch, "/api/v1/resources/"+created.ID, 1,
		updateResourceRequest{Status: &disabled}, map[string]string{"id": created.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := decodeResourceDTO(t, w).Status; got != int16(resource.StatusDisabled) {
		t.Fatalf("status = %d, want %d (disabled)", got, resource.StatusDisabled)
	}
}

func TestUpdateResource_WrongOwnerReturns404(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	created := createTestResource(t, h, string(resource.KindTool), "owner-only", map[string]any{"endpoint": "https://x"})

	w := doResourceRequest(t, h.Update, http.MethodPatch, "/api/v1/resources/"+created.ID, 999,
		updateResourceRequest{}, map[string]string{"id": created.ID})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (resource scoped to a different owner)", w.Code)
	}
}

func decodeDeleteCheck(t *testing.T, w *httptest.ResponseRecorder) deleteCheckDTO {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var check deleteCheckDTO
	if err := json.Unmarshal(dataBytes, &check); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return check
}

func TestDeleteCheck_DeletableWhenUnreferenced(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	created := createTestResource(t, h, string(resource.KindTool), "unreferenced", map[string]any{"endpoint": "https://x"})

	w := doResourceRequest(t, h.DeleteCheck, http.MethodGet, "/api/v1/resources/"+created.ID+"/delete-check", 1,
		nil, map[string]string{"id": created.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	check := decodeDeleteCheck(t, w)
	if !check.Deletable {
		t.Fatalf("expected deletable=true for an unreferenced resource: %+v", check)
	}
	if check.ReferencedBy == nil {
		t.Fatal("referenced_by must serialise as [] rather than null")
	}
}

func TestDeleteCheck_NotDeletableWhenReferenced(t *testing.T) {
	h, repo := newResourceHandlersForTest()
	created := createTestResource(t, h, string(resource.KindTool), "referenced-tool", map[string]any{"endpoint": "https://x"})
	repo.references["referenced-tool"] = []resource.AgentReference{{AgentRef: "architect", Version: "1.0.0"}}

	w := doResourceRequest(t, h.DeleteCheck, http.MethodGet, "/api/v1/resources/"+created.ID+"/delete-check", 1,
		nil, map[string]string{"id": created.ID})
	check := decodeDeleteCheck(t, w)
	if check.Deletable {
		t.Fatal("expected deletable=false when an agent references the resource")
	}
	if len(check.ReferencedBy) != 1 || check.ReferencedBy[0].Ref != "architect" || check.ReferencedBy[0].Type != "agent" {
		t.Fatalf("unexpected referenced_by: %+v", check.ReferencedBy)
	}
}

func TestListResources_FiltersByType(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	createTestResource(t, h, string(resource.KindTool), "t1", map[string]any{"endpoint": "https://x"})
	createTestResource(t, h, string(resource.KindSkill), "s1", map[string]any{"x": "y"})

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
	if len(page.Items) != 1 || page.Items[0].Type != string(resource.KindTool) {
		t.Fatalf("expected exactly 1 tool, got %+v", page.Items)
	}
}

func TestListResources_UnknownTypeReturns400(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	w := doResourceRequest(t, h.List, http.MethodGet, "/api/v1/resources?type=wormhole", 1, nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
