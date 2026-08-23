package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/operation"
)

// The moderation rules — the admin gate, what a takedown reaches, when a
// report may still be resolved — are tested against the service in
// internal/domain/operation. This covers transport: DTO shapes and the
// status codes produced before the service is reached.

type stubReports struct {
	byID   map[int64]operation.Report
	nextID int64
}

func (s *stubReports) Create(_ context.Context, listingID, reporterUserID int64, reason string) (operation.Report, error) {
	r := operation.Report{ID: s.nextID, ListingID: listingID, ReporterUserID: reporterUserID, Reason: reason, Status: operation.ReportPending}
	s.nextID++
	s.byID[r.ID] = r
	return r, nil
}

func (s *stubReports) Get(_ context.Context, id int64) (operation.Report, error) {
	r, ok := s.byID[id]
	if !ok {
		return operation.Report{}, operation.ErrNotFound
	}
	return r, nil
}

func (s *stubReports) ListPending(_ context.Context, beforeID int64, limit int) ([]operation.Report, error) {
	var out []operation.Report
	for id := int64(1); id < s.nextID && len(out) < limit; id++ {
		if r, ok := s.byID[id]; ok && r.Pending() && r.ID < beforeID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *stubReports) Resolve(_ context.Context, id int64, res operation.Resolution, _ int64) (operation.Report, error) {
	r := s.byID[id]
	r.Status, r.Resolution = operation.ReportResolved, &res
	s.byID[id] = r
	return r, nil
}

type stubAuditReader struct{ entries []operation.AuditEntry }

func (s *stubAuditReader) ListForActor(_ context.Context, _, beforeID int64, limit int) ([]operation.AuditEntry, error) {
	var out []operation.AuditEntry
	for _, e := range s.entries {
		if e.ID < beforeID && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type stubAuditWriter struct{}

func (stubAuditWriter) Record(context.Context, *int64, string, string, string, map[string]any) error {
	return nil
}

type stubListings struct{ listing operation.Listing }

func (s *stubListings) ByRef(_ context.Context, ref string) (operation.Listing, error) {
	if s.listing.Ref != ref {
		return operation.Listing{}, operation.ErrNotFound
	}
	return s.listing, nil
}

func (s *stubListings) ByID(_ context.Context, id int64) (operation.Listing, error) {
	if s.listing.ID != id {
		return operation.Listing{}, operation.ErrNotFound
	}
	return s.listing, nil
}

func (s *stubListings) Stop(context.Context, int64) error { return nil }

type stubDisabler struct{}

func (stubDisabler) Disable(context.Context, string, int64) error { return nil }

type stubAdmins struct{ admins map[int64]bool }

func (s stubAdmins) IsAdmin(_ context.Context, userID int64) (bool, error) {
	return s.admins[userID], nil
}

const testAdminID int64 = 1

type operationFixture struct {
	handlers *OperationHandlers
	reports  *stubReports
	audit    *stubAuditReader
	listings *stubListings
}

func newOperationFixture() *operationFixture {
	reports := &stubReports{byID: map[int64]operation.Report{}, nextID: 1}
	audit := &stubAuditReader{}
	listings := &stubListings{listing: operation.Listing{ID: 10, Ref: "some-bundle", Kind: "bundle", ResourceID: 77, SubscriberCount: 12}}
	svc := operation.NewService(reports, audit, stubAuditWriter{}, listings, stubDisabler{}, stubAdmins{admins: map[int64]bool{testAdminID: true}})
	return &operationFixture{handlers: NewOperationHandlers(svc), reports: reports, audit: audit, listings: listings}
}

func operationRequest(method, url string, userID int64, params map[string]string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}
	return r.WithContext(WithUserID(r.Context(), userID))
}

func decodeReportDTO(t *testing.T, w *httptest.ResponseRecorder) reportDTO {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var dto reportDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return dto
}

func TestSubmitReport_ResponseShape(t *testing.T) {
	f := newOperationFixture()

	w := httptest.NewRecorder()
	f.handlers.SubmitReport(w, operationRequest(http.MethodPost, "/marketplace/listings/some-bundle/report", 5,
		map[string]string{"ref": "some-bundle"}, []byte(`{"reason":"抄袭"}`)))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	dto := decodeReportDTO(t, w)
	if dto.ListingRef != "some-bundle" || dto.Status != string(operation.ReportPending) || dto.SubscriberCount != 12 {
		t.Fatalf("unexpected report: %+v", dto)
	}
	if dto.Resolution != nil || dto.ResolvedAt != nil {
		t.Fatalf("a pending report must send null for both resolution fields: %+v", dto)
	}
}

func TestSubmitReport_MalformedBodyReturns400WithDetails(t *testing.T) {
	f := newOperationFixture()

	w := httptest.NewRecorder()
	f.handlers.SubmitReport(w, operationRequest(http.MethodPost, "/marketplace/listings/some-bundle/report", 5,
		map[string]string{"ref": "some-bundle"}, []byte("{not json")))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Details) == 0 {
		t.Fatal("expected field-level details")
	}
}

func TestSubmitReport_UnknownListingReturns404(t *testing.T) {
	f := newOperationFixture()

	w := httptest.NewRecorder()
	f.handlers.SubmitReport(w, operationRequest(http.MethodPost, "/marketplace/listings/nope/report", 5,
		map[string]string{"ref": "nope"}, []byte(`{"reason":"spam"}`)))

	if w.Code != http.StatusNotFound || !containsCode(w.Body.String(), ErrListingNotFound) {
		t.Fatalf("expected 404/70002, got %d: %s", w.Code, w.Body.String())
	}
}

func TestModerationEndpoints_NonAdminReturns403(t *testing.T) {
	f := newOperationFixture()
	_, _ = f.reports.Create(context.Background(), 10, 5, "spam")

	w := httptest.NewRecorder()
	f.handlers.ListPendingReports(w, operationRequest(http.MethodGet, "/moderation/reports", 5, nil, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("list: status = %d, want 403: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	f.handlers.ResolveReport(w, operationRequest(http.MethodPost, "/moderation/reports/1/resolve", 5,
		map[string]string{"id": "1"}, []byte(`{"action":"takedown"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("resolve: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestListPendingReports_AdminSeesTheQueue(t *testing.T) {
	f := newOperationFixture()
	_, _ = f.reports.Create(context.Background(), 10, 5, "抄袭")

	w := httptest.NewRecorder()
	f.handlers.ListPendingReports(w, operationRequest(http.MethodGet, "/moderation/reports", testAdminID, nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var page struct {
		Items []reportDTO `json:"items"`
	}
	_ = json.Unmarshal(dataBytes, &page)
	if len(page.Items) != 1 || page.Items[0].ListingRef != "some-bundle" {
		t.Fatalf("unexpected queue: %+v", page.Items)
	}
}

func TestResolveReport_ResponseCarriesTheResolution(t *testing.T) {
	f := newOperationFixture()
	_, _ = f.reports.Create(context.Background(), 10, 5, "抄袭")

	w := httptest.NewRecorder()
	f.handlers.ResolveReport(w, operationRequest(http.MethodPost, "/moderation/reports/1/resolve", testAdminID,
		map[string]string{"id": "1"}, []byte(`{"action":"takedown"}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	dto := decodeReportDTO(t, w)
	if dto.Status != string(operation.ReportResolved) || dto.Resolution == nil || *dto.Resolution != "takedown" {
		t.Fatalf("unexpected report: %+v", dto)
	}
}

func TestResolveReport_NonNumericIDReturns400(t *testing.T) {
	f := newOperationFixture()

	w := httptest.NewRecorder()
	f.handlers.ResolveReport(w, operationRequest(http.MethodPost, "/moderation/reports/abc/resolve", testAdminID,
		map[string]string{"id": "abc"}, []byte(`{"action":"dismiss"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestListMyAuditLogs_ResponseShape(t *testing.T) {
	f := newOperationFixture()
	f.audit.entries = []operation.AuditEntry{
		{ID: 2, Action: "human_gate.approved", TargetType: "human_gate", TargetID: "5", Detail: json.RawMessage(`{"node":"review"}`)},
	}

	w := httptest.NewRecorder()
	f.handlers.ListMyAuditLogs(w, operationRequest(http.MethodGet, "/audit-logs", 5, nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var page struct {
		Items []auditLogDTO `json:"items"`
	}
	_ = json.Unmarshal(dataBytes, &page)
	if len(page.Items) != 1 || page.Items[0].ID != "2" || page.Items[0].Action != "human_gate.approved" {
		t.Fatalf("unexpected entries: %+v", page.Items)
	}
	// Detail is passed through as raw JSON: its shape belongs to whichever
	// context wrote the entry, not to this one.
	if string(page.Items[0].Detail) != `{"node":"review"}` {
		t.Fatalf("detail should pass through untouched, got %s", page.Items[0].Detail)
	}
}

func TestListMyAuditLogs_InvalidCursorReturns400(t *testing.T) {
	f := newOperationFixture()

	w := httptest.NewRecorder()
	f.handlers.ListMyAuditLogs(w, operationRequest(http.MethodGet, "/audit-logs?cursor=!!!not-base64!!!", 5, nil, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestOperationHandlers_RequireAuthenticatedUser(t *testing.T) {
	f := newOperationFixture()
	for name, handler := range map[string]http.HandlerFunc{
		"audit-logs": f.handlers.ListMyAuditLogs, "report": f.handlers.SubmitReport,
		"queue": f.handlers.ListPendingReports, "resolve": f.handlers.ResolveReport,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/audit-logs", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a user: status = %d, want 401", name, w.Code)
		}
	}
}
