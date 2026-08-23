package operation_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/operation"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeReports struct {
	byID    map[int64]operation.Report
	pending []operation.Report
	nextID  int64
	created []operation.Report
}

func newFakeReports() *fakeReports {
	return &fakeReports{byID: map[int64]operation.Report{}, nextID: 1}
}

func (f *fakeReports) Create(_ context.Context, listingID, reporterUserID int64, reason string) (operation.Report, error) {
	r := operation.Report{ID: f.nextID, ListingID: listingID, ReporterUserID: reporterUserID, Reason: reason, Status: operation.ReportPending}
	f.nextID++
	f.byID[r.ID] = r
	f.created = append(f.created, r)
	return r, nil
}

func (f *fakeReports) Get(_ context.Context, id int64) (operation.Report, error) {
	r, ok := f.byID[id]
	if !ok {
		return operation.Report{}, operation.ErrNotFound
	}
	return r, nil
}

func (f *fakeReports) ListPending(_ context.Context, beforeID int64, limit int) ([]operation.Report, error) {
	var out []operation.Report
	for _, r := range f.pending {
		if r.ID < beforeID && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeReports) Resolve(_ context.Context, id int64, res operation.Resolution, resolvedBy int64) (operation.Report, error) {
	r := f.byID[id]
	r.Status, r.Resolution = operation.ReportResolved, &res
	f.byID[id] = r
	return r, nil
}

type fakeAuditReader struct {
	byActor map[int64][]operation.AuditEntry
}

func (f *fakeAuditReader) ListForActor(_ context.Context, actorUserID, beforeID int64, limit int) ([]operation.AuditEntry, error) {
	var out []operation.AuditEntry
	for _, e := range f.byActor[actorUserID] {
		if e.ID < beforeID && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type auditRecord struct {
	actor  *int64
	action string
	target string
	detail map[string]any
}

type fakeAuditWriter struct{ records []auditRecord }

func (f *fakeAuditWriter) Record(_ context.Context, actor *int64, action, _, targetID string, detail map[string]any) error {
	f.records = append(f.records, auditRecord{actor: actor, action: action, target: targetID, detail: detail})
	return nil
}

type fakeListings struct {
	byRef   map[string]operation.Listing
	byID    map[int64]operation.Listing
	stopped []int64
}

func (f *fakeListings) ByRef(_ context.Context, ref string) (operation.Listing, error) {
	l, ok := f.byRef[ref]
	if !ok {
		return operation.Listing{}, operation.ErrNotFound
	}
	return l, nil
}

func (f *fakeListings) ByID(_ context.Context, id int64) (operation.Listing, error) {
	l, ok := f.byID[id]
	if !ok {
		return operation.Listing{}, operation.ErrNotFound
	}
	return l, nil
}

func (f *fakeListings) Stop(_ context.Context, id int64) error {
	f.stopped = append(f.stopped, id)
	return nil
}

type disabled struct {
	kind string
	id   int64
}

type fakeDisabler struct {
	calls []disabled
	err   error
}

func (f *fakeDisabler) Disable(_ context.Context, kind string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, disabled{kind: kind, id: id})
	return nil
}

type fakeAdmins struct {
	admins map[int64]bool
	err    error
}

func (f *fakeAdmins) IsAdmin(_ context.Context, userID int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.admins[userID], nil
}

type harness struct {
	svc      *operation.Service
	reports  *fakeReports
	audit    *fakeAuditReader
	auditLog *fakeAuditWriter
	listings *fakeListings
	disabler *fakeDisabler
	admins   *fakeAdmins
}

const adminID int64 = 1

func newHarness() *harness {
	h := &harness{
		reports:  newFakeReports(),
		audit:    &fakeAuditReader{byActor: map[int64][]operation.AuditEntry{}},
		auditLog: &fakeAuditWriter{},
		listings: &fakeListings{byRef: map[string]operation.Listing{}, byID: map[int64]operation.Listing{}},
		disabler: &fakeDisabler{},
		admins:   &fakeAdmins{admins: map[int64]bool{adminID: true}},
	}
	h.svc = operation.NewService(h.reports, h.audit, h.auditLog, h.listings, h.disabler, h.admins)
	return h
}

func (h *harness) addListing(l operation.Listing) {
	h.listings.byRef[l.Ref] = l
	h.listings.byID[l.ID] = l
}

func mustDomainErr(t *testing.T, err error) *domain.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("expected a domain error, got %T: %v", err, err)
	}
	return de
}

// ── Reports ──────────────────────────────────────────────────────────

func TestSubmitReport_RequiresAReason(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "some-bundle"})

	de := mustDomainErr(t, func() error { _, err := h.svc.SubmitReport(context.Background(), 5, "some-bundle", ""); return err }())
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 1 || de.Details[0].Field != "reason" {
		t.Fatalf("expected a reason field error, got %+v", de)
	}
	if len(h.reports.created) != 0 {
		t.Fatal("a rejected report must not be stored")
	}
}

func TestSubmitReport_UnknownListingIsNotFound(t *testing.T) {
	h := newHarness()
	de := mustDomainErr(t, func() error { _, err := h.svc.SubmitReport(context.Background(), 5, "nope", "spam"); return err }())
	if de.Kind != domain.KindNotFound || de.Code != domain.CodeListingNotFound {
		t.Fatalf("expected 404/70002, got kind=%v code=%d", de.Kind, de.Code)
	}
}

// Anyone authenticated may report — including a non-subscriber, since the
// listings most worth reporting are the ones nobody has committed to yet.
func TestSubmitReport_AnyAuthenticatedUserMayReport(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "some-bundle", SubscriberCount: 7})

	view, err := h.svc.SubmitReport(context.Background(), 5, "some-bundle", "抄袭")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if view.Report.Status != operation.ReportPending || view.Report.ReporterUserID != 5 {
		t.Fatalf("unexpected report: %+v", view.Report)
	}
	if view.Listing.Ref != "some-bundle" || view.Listing.SubscriberCount != 7 {
		t.Fatalf("the view should carry the listing it names: %+v", view.Listing)
	}
}

// ── Admin gate ───────────────────────────────────────────────────────

func TestModeration_RequiresAdmin(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "b"})
	_, _ = h.reports.Create(context.Background(), 10, 5, "spam")

	if _, err := h.svc.ListPendingReports(context.Background(), 5, domain.PageQuery{}); true {
		de := mustDomainErr(t, err)
		if de.Kind != domain.KindForbidden || de.Code != domain.CodeForbidden {
			t.Fatalf("list: expected 403/20003, got kind=%v code=%d", de.Kind, de.Code)
		}
	}
	if _, err := h.svc.ResolveReport(context.Background(), 5, 1, "takedown"); true {
		de := mustDomainErr(t, err)
		if de.Kind != domain.KindForbidden {
			t.Fatalf("resolve: expected 403, got kind=%v", de.Kind)
		}
	}
	if len(h.disabler.calls) != 0 {
		t.Fatal("a non-admin must not be able to take anything down")
	}
}

// The admin check fails closed: if the directory cannot answer, the answer
// is no. A moderation action is not the place to give the benefit of the
// doubt.
func TestModeration_AdminLookupFailureIsRefusal(t *testing.T) {
	h := newHarness()
	h.admins.err = errors.New("directory unavailable")

	de := mustDomainErr(t, func() error {
		_, err := h.svc.ListPendingReports(context.Background(), adminID, domain.PageQuery{})
		return err
	}())
	if de.Kind != domain.KindForbidden {
		t.Fatalf("expected a refusal rather than a 500, got kind=%v", de.Kind)
	}
}

// ── Resolution ───────────────────────────────────────────────────────

// A takedown must reach past the listing to the resource itself:
// distribution alone would leave existing subscribers, who hold a
// snapshot, still able to run it.
func TestResolveReport_TakedownDisablesResourceAndStopsListing(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "bad-bundle", Kind: "bundle", ResourceID: 77, SubscriberCount: 12})
	_, _ = h.reports.Create(context.Background(), 10, 5, "抄袭")

	view, err := h.svc.ResolveReport(context.Background(), adminID, 1, "takedown")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if view.Report.Status != operation.ReportResolved || *view.Report.Resolution != operation.ResolutionTakedown {
		t.Fatalf("unexpected resolution: %+v", view.Report)
	}
	if len(h.disabler.calls) != 1 || h.disabler.calls[0] != (disabled{kind: "bundle", id: 77}) {
		t.Fatalf("expected the underlying resource to be disabled: %+v", h.disabler.calls)
	}
	if len(h.listings.stopped) != 1 || h.listings.stopped[0] != 10 {
		t.Fatalf("expected the listing to be stopped: %+v", h.listings.stopped)
	}

	if len(h.auditLog.records) != 1 {
		t.Fatalf("a takedown must be auditable: %+v", h.auditLog.records)
	}
	rec := h.auditLog.records[0]
	if rec.action != operation.ActionTakedown || rec.actor == nil || *rec.actor != adminID {
		t.Fatalf("audit entry should name the admin who acted: %+v", rec)
	}
	if rec.detail["subscriber_count"] != int32(12) {
		t.Fatalf("the audit entry should record how many people this affected: %+v", rec.detail)
	}
}

// If disabling the resource fails, the listing stays up rather than
// leaving a listing nobody can act on pointing at a live resource.
func TestResolveReport_TakedownStopsIfTheResourceCannotBeDisabled(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "bad-bundle", Kind: "bundle", ResourceID: 77})
	_, _ = h.reports.Create(context.Background(), 10, 5, "抄袭")
	h.disabler.err = errors.New("storage down")

	de := mustDomainErr(t, func() error { _, err := h.svc.ResolveReport(context.Background(), adminID, 1, "takedown"); return err }())
	if de.Kind != domain.KindInternal {
		t.Fatalf("expected an internal error, got kind=%v", de.Kind)
	}
	if len(h.listings.stopped) != 0 {
		t.Fatal("the listing must not be stopped when the resource is still enabled")
	}
	if h.reports.byID[1].Status != operation.ReportPending {
		t.Fatal("a failed takedown must leave the report open for another attempt")
	}
}

func TestResolveReport_DismissLeavesEverythingAlone(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "fine-bundle", Kind: "bundle", ResourceID: 77})
	_, _ = h.reports.Create(context.Background(), 10, 5, "不实举报")

	view, err := h.svc.ResolveReport(context.Background(), adminID, 1, "dismiss")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if *view.Report.Resolution != operation.ResolutionDismiss {
		t.Fatalf("unexpected resolution: %+v", view.Report)
	}
	if len(h.disabler.calls) != 0 || len(h.listings.stopped) != 0 || len(h.auditLog.records) != 0 {
		t.Fatal("dismissing a report must not touch the listing or its resource")
	}
}

func TestResolveReport_UnknownActionIsRejected(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "b", Kind: "bundle", ResourceID: 77})
	_, _ = h.reports.Create(context.Background(), 10, 5, "spam")

	for _, action := range []string{"", "delete", "TAKEDOWN"} {
		de := mustDomainErr(t, func() error { _, err := h.svc.ResolveReport(context.Background(), adminID, 1, action); return err }())
		if de.Code != domain.CodeValidationFailed || len(de.Details) != 1 || de.Details[0].Field != "action" {
			t.Fatalf("action %q: expected an action field error, got %+v", action, de)
		}
	}
}

func TestResolveReport_UnknownReportIsNotFound(t *testing.T) {
	h := newHarness()
	de := mustDomainErr(t, func() error { _, err := h.svc.ResolveReport(context.Background(), adminID, 999, "dismiss"); return err }())
	if de.Kind != domain.KindNotFound || de.Code != domain.CodeReportNotFound {
		t.Fatalf("expected 404/80001, got kind=%v code=%d", de.Kind, de.Code)
	}
}

func TestResolveReport_AlreadyResolvedConflicts(t *testing.T) {
	h := newHarness()
	h.addListing(operation.Listing{ID: 10, Ref: "b", Kind: "bundle", ResourceID: 77})
	_, _ = h.reports.Create(context.Background(), 10, 5, "spam")
	if _, err := h.svc.ResolveReport(context.Background(), adminID, 1, "dismiss"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	de := mustDomainErr(t, func() error { _, err := h.svc.ResolveReport(context.Background(), adminID, 1, "takedown"); return err }())
	if de.Kind != domain.KindConflict || de.Code != domain.CodeReportAlreadyResolved {
		t.Fatalf("expected 409/80002, got kind=%v code=%d", de.Kind, de.Code)
	}
	if len(h.disabler.calls) != 0 {
		t.Fatal("re-resolving a closed report must not take anything down")
	}
}

// ── Audit log ────────────────────────────────────────────────────────

func TestListMyAuditLog_IsScopedToTheCaller(t *testing.T) {
	h := newHarness()
	h.audit.byActor[5] = []operation.AuditEntry{{ID: 2, Action: "human_gate.approved", Detail: json.RawMessage(`{}`)}}
	h.audit.byActor[6] = []operation.AuditEntry{{ID: 1, Action: "human_gate.rejected", Detail: json.RawMessage(`{}`)}}

	page, err := h.svc.ListMyAuditLog(context.Background(), 5, domain.PageQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Action != "human_gate.approved" {
		t.Fatalf("expected only the caller's own entries: %+v", page.Items)
	}
}

// Both listings here run newest-first, so an absent cursor must start at
// the top of the range rather than at id 0 — which would return nothing.
func TestListMyAuditLog_AbsentCursorStartsAtTheNewest(t *testing.T) {
	h := newHarness()
	h.audit.byActor[5] = []operation.AuditEntry{{ID: 900}, {ID: 899}}

	page, err := h.svc.ListMyAuditLog(context.Background(), 5, domain.PageQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected the newest entries, got %+v", page.Items)
	}
}

func TestListMyAuditLog_PaginatesDownwards(t *testing.T) {
	h := newHarness()
	var entries []operation.AuditEntry
	for id := int64(100); id > 0; id-- {
		entries = append(entries, operation.AuditEntry{ID: id})
	}
	h.audit.byActor[5] = entries

	page, err := h.svc.ListMyAuditLog(context.Background(), 5, domain.PageQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 20 || !page.HasMore {
		t.Fatalf("expected a full page with more to come: %d items, has_more=%v", len(page.Items), page.HasMore)
	}
	if page.NextCursor != "81" {
		t.Fatalf("next cursor = %q, want the last id on this page", page.NextCursor)
	}

	next, err := h.svc.ListMyAuditLog(context.Background(), 5, domain.PageQuery{Limit: 20, After: page.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(next.Items) != 20 || next.Items[0].ID != 80 {
		t.Fatalf("the second page should continue below the cursor, got first id %d", next.Items[0].ID)
	}
}

func TestListMyAuditLog_EmptyPageIsAnEmptySlice(t *testing.T) {
	h := newHarness()
	page, err := h.svc.ListMyAuditLog(context.Background(), 5, domain.PageQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Items == nil {
		t.Fatal("items must serialise as [] rather than null")
	}
}
