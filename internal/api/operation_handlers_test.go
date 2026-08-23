package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

type fakeOperationQuerier struct {
	store.Querier

	users        map[int64]store.User
	listings     map[int64]store.MarketplaceListing
	auditLogs    []store.AuditLog
	reports      map[int64]store.Report
	nextReportID int64

	agentStatus  map[int64]int16
	bundleStatus map[int64]int16
	skillStatus  map[int64]int16
	mcpStatus    map[int64]int16
}

func newFakeOperationQuerier() *fakeOperationQuerier {
	return &fakeOperationQuerier{
		users: map[int64]store.User{}, listings: map[int64]store.MarketplaceListing{},
		reports: map[int64]store.Report{}, nextReportID: 1,
		agentStatus: map[int64]int16{}, bundleStatus: map[int64]int16{},
		skillStatus: map[int64]int16{}, mcpStatus: map[int64]int16{},
	}
}

func (f *fakeOperationQuerier) GetUserByID(_ context.Context, id int64) (store.User, error) {
	u, ok := f.users[id]
	if !ok {
		return store.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeOperationQuerier) GetListingByListingRefLatestPublished(_ context.Context, ref string) (store.MarketplaceListing, error) {
	for _, l := range f.listings {
		if l.ListingRef == ref {
			return l, nil
		}
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeOperationQuerier) GetListingByID(_ context.Context, id int64) (store.MarketplaceListing, error) {
	l, ok := f.listings[id]
	if !ok {
		return store.MarketplaceListing{}, pgx.ErrNoRows
	}
	return l, nil
}

func (f *fakeOperationQuerier) SetListingDistribution(_ context.Context, arg store.SetListingDistributionParams) error {
	l := f.listings[arg.ID]
	l.Distribution = arg.Distribution
	f.listings[arg.ID] = l
	return nil
}

func (f *fakeOperationQuerier) CreateAuditLog(_ context.Context, arg store.CreateAuditLogParams) (store.AuditLog, error) {
	log := store.AuditLog{ID: int64(len(f.auditLogs) + 1), ActorUserID: arg.ActorUserID, Action: arg.Action, TargetType: arg.TargetType, TargetID: arg.TargetID, Detail: arg.Detail}
	f.auditLogs = append(f.auditLogs, log)
	return log, nil
}

func (f *fakeOperationQuerier) ListAuditLogsForActorPage(_ context.Context, arg store.ListAuditLogsForActorPageParams) ([]store.AuditLog, error) {
	var out []store.AuditLog
	for i := len(f.auditLogs) - 1; i >= 0; i-- {
		l := f.auditLogs[i]
		if l.ActorUserID != arg.ActorUserID {
			continue
		}
		if l.ID >= arg.ID {
			continue
		}
		out = append(out, l)
		if int32(len(out)) >= arg.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeOperationQuerier) CreateReport(_ context.Context, arg store.CreateReportParams) (store.Report, error) {
	rep := store.Report{ID: f.nextReportID, ListingID: arg.ListingID, ReporterUserID: arg.ReporterUserID, Reason: arg.Reason, Status: "pending"}
	f.reports[rep.ID] = rep
	f.nextReportID++
	return rep, nil
}

func (f *fakeOperationQuerier) GetReportByID(_ context.Context, id int64) (store.Report, error) {
	r, ok := f.reports[id]
	if !ok {
		return store.Report{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeOperationQuerier) ListPendingReportsPage(_ context.Context, arg store.ListPendingReportsPageParams) ([]store.Report, error) {
	var out []store.Report
	for id := f.nextReportID - 1; id >= 1; id-- {
		r, ok := f.reports[id]
		if !ok || r.Status != "pending" || r.ID >= arg.ID {
			continue
		}
		out = append(out, r)
		if int32(len(out)) >= arg.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeOperationQuerier) ResolveReport(_ context.Context, arg store.ResolveReportParams) (store.Report, error) {
	r, ok := f.reports[arg.ID]
	if !ok {
		return store.Report{}, pgx.ErrNoRows
	}
	r.Status = "resolved"
	r.Resolution = arg.Resolution
	r.ResolvedBy = arg.ResolvedBy
	f.reports[arg.ID] = r
	return r, nil
}

func (f *fakeOperationQuerier) SetAgentStatusByID(_ context.Context, arg store.SetAgentStatusByIDParams) error {
	f.agentStatus[arg.ID] = arg.Status
	return nil
}
func (f *fakeOperationQuerier) SetBundleStatusByID(_ context.Context, arg store.SetBundleStatusByIDParams) error {
	f.bundleStatus[arg.ID] = arg.Status
	return nil
}
func (f *fakeOperationQuerier) SetSkillStatusByID(_ context.Context, arg store.SetSkillStatusByIDParams) error {
	f.skillStatus[arg.ID] = arg.Status
	return nil
}
func (f *fakeOperationQuerier) SetMCPServerStatusByID(_ context.Context, arg store.SetMCPServerStatusByIDParams) error {
	f.mcpStatus[arg.ID] = arg.Status
	return nil
}

func doOperationRequest(h http.HandlerFunc, userID int64, method, path string, routeParams map[string]string, body any) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req = req.WithContext(WithUserID(req.Context(), userID))
	if len(routeParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range routeParams {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func decodeEnvelope(t *testing.T, rr *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, rr.Body.String())
	}
	return env
}

func TestSubmitReport_CreatesReportForExistingListing(t *testing.T) {
	f := newFakeOperationQuerier()
	f.listings[1] = store.MarketplaceListing{ID: 1, ListingRef: "shady-bundle", ResourceType: "bundle", ResourceID: 9, SubscriberCount: 3}
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.SubmitReport, 5, http.MethodPost, "/marketplace/listings/shady-bundle/report",
		map[string]string{"ref": "shady-bundle"}, createReportRequest{Reason: "抄袭"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(f.reports) != 1 {
		t.Fatalf("expected 1 report stored, got %d", len(f.reports))
	}
}

func TestSubmitReport_UnknownListing_404(t *testing.T) {
	f := newFakeOperationQuerier()
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.SubmitReport, 5, http.MethodPost, "/marketplace/listings/nope/report",
		map[string]string{"ref": "nope"}, createReportRequest{Reason: "x"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestListPendingReports_ForbidsNonAdmin(t *testing.T) {
	f := newFakeOperationQuerier()
	f.users[1] = store.User{ID: 1, IsAdmin: false}
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ListPendingReports, 1, http.MethodGet, "/moderation/reports", nil, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListPendingReports_AllowsAdmin(t *testing.T) {
	f := newFakeOperationQuerier()
	f.users[1] = store.User{ID: 1, IsAdmin: true}
	f.listings[1] = store.MarketplaceListing{ID: 1, ListingRef: "shady-bundle"}
	f.reports[1] = store.Report{ID: 1, ListingID: 1, Status: "pending", Reason: "抄袭"}
	f.nextReportID = 2
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ListPendingReports, 1, http.MethodGet, "/moderation/reports", nil, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	env := decodeEnvelope(t, rr)
	page, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data shape: %#v", env.Data)
	}
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 pending report, got %d", len(items))
	}
}

func TestResolveReport_TakedownDisablesUnderlyingResourceAndListing(t *testing.T) {
	f := newFakeOperationQuerier()
	f.users[1] = store.User{ID: 1, IsAdmin: true}
	f.listings[1] = store.MarketplaceListing{ID: 1, ListingRef: "shady-bundle", ResourceType: DepKindBundle, ResourceID: 42, Distribution: 1, SubscriberCount: 56}
	f.reports[1] = store.Report{ID: 1, ListingID: 1, Status: "pending"}
	f.nextReportID = 2
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ResolveReport, 1, http.MethodPost, "/moderation/reports/1/resolve",
		map[string]string{"id": "1"}, resolveReportRequest{Action: "takedown"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if f.bundleStatus[42] != 0 {
		t.Fatalf("expected underlying bundle disabled (status=0), got %d", f.bundleStatus[42])
	}
	if f.listings[1].Distribution != 3 {
		t.Fatalf("expected listing distribution set to 3 (admin takedown), got %d", f.listings[1].Distribution)
	}
	if f.reports[1].Status != "resolved" {
		t.Fatalf("expected report resolved, got %s", f.reports[1].Status)
	}
	if len(f.auditLogs) != 1 || f.auditLogs[0].Action != "moderation.takedown" {
		t.Fatalf("expected a moderation.takedown audit log entry, got %#v", f.auditLogs)
	}
}

func TestResolveReport_Dismiss_DoesNotTouchResource(t *testing.T) {
	f := newFakeOperationQuerier()
	f.users[1] = store.User{ID: 1, IsAdmin: true}
	f.listings[1] = store.MarketplaceListing{ID: 1, ListingRef: "fine-bundle", ResourceType: DepKindBundle, ResourceID: 42, Distribution: 1}
	f.reports[1] = store.Report{ID: 1, ListingID: 1, Status: "pending"}
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ResolveReport, 1, http.MethodPost, "/moderation/reports/1/resolve",
		map[string]string{"id": "1"}, resolveReportRequest{Action: "dismiss"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, touched := f.bundleStatus[42]; touched {
		t.Fatalf("dismiss must not disable the underlying resource")
	}
	if f.listings[1].Distribution != 1 {
		t.Fatalf("dismiss must not change listing distribution, got %d", f.listings[1].Distribution)
	}
}

func TestResolveReport_AlreadyResolved_Conflict(t *testing.T) {
	f := newFakeOperationQuerier()
	f.users[1] = store.User{ID: 1, IsAdmin: true}
	f.reports[1] = store.Report{ID: 1, ListingID: 1, Status: "resolved"}
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ResolveReport, 1, http.MethodPost, "/moderation/reports/1/resolve",
		map[string]string{"id": "1"}, resolveReportRequest{Action: "dismiss"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListMyAuditLogs_ScopedToRequestingUser(t *testing.T) {
	f := newFakeOperationQuerier()
	_, _ = f.CreateAuditLog(context.Background(), store.CreateAuditLogParams{ActorUserID: pgtype.Int8{Valid: true, Int64: 1}, Action: "human_gate.approved", TargetType: "human_gate", TargetID: "1"})
	_, _ = f.CreateAuditLog(context.Background(), store.CreateAuditLogParams{ActorUserID: pgtype.Int8{Valid: true, Int64: 2}, Action: "human_gate.approved", TargetType: "human_gate", TargetID: "2"})
	h := NewOperationHandlers(f)

	rr := doOperationRequest(h.ListMyAuditLogs, 1, http.MethodGet, "/audit-logs", nil, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	env := decodeEnvelope(t, rr)
	page, _ := env.Data.(map[string]any)
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 log scoped to user 1, got %d", len(items))
	}
}
