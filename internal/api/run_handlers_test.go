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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// fakeRunHandlersQuerier is an in-memory store.Querier covering exactly
// what run_handlers.go / run_gate_handlers.go call.
type fakeRunHandlersQuerier struct {
	store.Querier

	runs       map[string]store.BundleRun
	bundles    map[int64]store.Bundle
	gates      map[int64]store.HumanGate
	nextGateID int64
	audits     []store.AuditLog
	events     []store.BundleRunEvent

	cancelRequested map[string]bool
}

func newFakeRunHandlersQuerier() *fakeRunHandlersQuerier {
	return &fakeRunHandlersQuerier{
		runs: map[string]store.BundleRun{}, bundles: map[int64]store.Bundle{},
		gates: map[int64]store.HumanGate{}, nextGateID: 1,
		cancelRequested: map[string]bool{},
	}
}

func (f *fakeRunHandlersQuerier) GetBundleRun(_ context.Context, id string) (store.BundleRun, error) {
	if r, ok := f.runs[id]; ok {
		return r, nil
	}
	return store.BundleRun{}, pgx.ErrNoRows
}

func (f *fakeRunHandlersQuerier) GetBundleByID(_ context.Context, id int64) (store.Bundle, error) {
	if b, ok := f.bundles[id]; ok {
		return b, nil
	}
	return store.Bundle{}, pgx.ErrNoRows
}

func (f *fakeRunHandlersQuerier) MarkBundleRunCancelRequested(_ context.Context, id string) error {
	f.cancelRequested[id] = true
	return nil
}

func (f *fakeRunHandlersQuerier) ListBundleRunEventsAfter(_ context.Context, arg store.ListBundleRunEventsAfterParams) ([]store.BundleRunEvent, error) {
	var out []store.BundleRunEvent
	for _, ev := range f.events {
		if ev.RunID == arg.RunID && ev.ID > arg.ID {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (f *fakeRunHandlersQuerier) ListBundleRunEventsAfterExternal(_ context.Context, arg store.ListBundleRunEventsAfterExternalParams) ([]store.BundleRunEvent, error) {
	var out []store.BundleRunEvent
	for _, ev := range f.events {
		if ev.RunID == arg.RunID && ev.ID > arg.ID && !ev.IsInternal {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (f *fakeRunHandlersQuerier) GetPendingHumanGateForRunNode(_ context.Context, arg store.GetPendingHumanGateForRunNodeParams) (store.HumanGate, error) {
	for i := f.nextGateID - 1; i >= 1; i-- {
		g, ok := f.gates[i]
		if ok && g.RunID == arg.RunID && g.Node == arg.Node && g.Status == "pending" {
			return g, nil
		}
	}
	return store.HumanGate{}, pgx.ErrNoRows
}

func (f *fakeRunHandlersQuerier) ResolveHumanGate(_ context.Context, arg store.ResolveHumanGateParams) (store.HumanGate, error) {
	g, ok := f.gates[arg.ID]
	if !ok || g.Status != "pending" {
		return store.HumanGate{}, pgx.ErrNoRows
	}
	g.Status = arg.Status
	g.ResolvedBy = arg.ResolvedBy
	g.Comment = arg.Comment
	f.gates[arg.ID] = g
	return g, nil
}

func (f *fakeRunHandlersQuerier) InsertBundleRunEvent(_ context.Context, arg store.InsertBundleRunEventParams) (store.BundleRunEvent, error) {
	ev := store.BundleRunEvent{ID: int64(len(f.events) + 1), RunID: arg.RunID, Type: arg.Type, Node: arg.Node, Payload: arg.Payload, IsInternal: arg.IsInternal}
	f.events = append(f.events, ev)
	return ev, nil
}

func (f *fakeRunHandlersQuerier) CreateAuditLog(_ context.Context, arg store.CreateAuditLogParams) (store.AuditLog, error) {
	row := store.AuditLog{ID: int64(len(f.audits) + 1), ActorUserID: arg.ActorUserID, Action: arg.Action, TargetType: arg.TargetType, TargetID: arg.TargetID, Detail: arg.Detail}
	f.audits = append(f.audits, row)
	return row, nil
}

func (f *fakeRunHandlersQuerier) addGate(id int64, runID, node, status string) store.HumanGate {
	g := store.HumanGate{ID: id, RunID: runID, Node: node, Status: status, OnTimeout: "abort", ApproverRoles: []byte("[]")}
	f.gates[id] = g
	if id >= f.nextGateID {
		f.nextGateID = id + 1
	}
	return g
}

func newTestRunHandlers(f *fakeRunHandlersQuerier) *RunHandlers {
	engine := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))
	return NewRunHandlers(f, engine)
}

func withUser(r *http.Request, userID int64) *http.Request {
	return r.WithContext(WithUserID(r.Context(), userID))
}

// ── Get: shared_state black-box filtering ───────────────────────────

func TestRunHandlers_Get_FiltersSharedStateForSubscriber(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	sharedState, _ := json.Marshal(map[string]any{"final_answer": "42", "internal_scratch": "secret prompt"})
	displayMeta, _ := json.Marshal(map[string]any{"io_description": map[string]any{"outputs": []string{"final_answer"}}})
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 99, BundleRef: "b1", Version: "v1", DisplayMeta: displayMeta}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 30, Status: "finished", SharedState: sharedState, CreatedAt: pgtype.Timestamptz{Valid: true}}

	h := newTestRunHandlers(f)
	req := httptest.NewRequest(http.MethodGet, "/runs/run-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withUser(req, 30)

	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			SharedState map[string]any `json:"shared_state"`
			IsOwner     bool           `json:"is_owner"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, leaked := body.Data.SharedState["internal_scratch"]; leaked {
		t.Fatalf("internal_scratch leaked to subscriber: %+v", body.Data.SharedState)
	}
	if body.Data.SharedState["final_answer"] != "42" {
		t.Fatalf("expected declared output to be visible, got %+v", body.Data.SharedState)
	}
	if body.Data.IsOwner {
		t.Fatal("expected is_owner=false for a subscriber, so the frontend renders black-box chrome")
	}
}

func TestRunHandlers_Get_OwnerSeesFullSharedState(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	sharedState, _ := json.Marshal(map[string]any{"final_answer": "42", "internal_scratch": "secret prompt"})
	displayMeta, _ := json.Marshal(map[string]any{"io_description": map[string]any{"outputs": []string{"final_answer"}}})
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 99, BundleRef: "b1", Version: "v1", DisplayMeta: displayMeta}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 99, Status: "finished", SharedState: sharedState, CreatedAt: pgtype.Timestamptz{Valid: true}}

	h := newTestRunHandlers(f)
	req := httptest.NewRequest(http.MethodGet, "/runs/run-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withUser(req, 99)

	w := httptest.NewRecorder()
	h.Get(w, req)

	var body struct {
		Data struct {
			SharedState map[string]any `json:"shared_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.SharedState["internal_scratch"] != "secret prompt" {
		t.Fatalf("owner should see full shared_state, got %+v", body.Data.SharedState)
	}
}

// ── Cancel ───────────────────────────────────────────────────────────

func TestRunHandlers_Cancel_AlreadyFinished(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 5, BundleRef: "b1", Version: "v1", DisplayMeta: []byte("{}")}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: "finished", SharedState: []byte("{}"), CreatedAt: pgtype.Timestamptz{Valid: true}}

	h := newTestRunHandlers(f)
	req := httptest.NewRequest(http.MethodPost, "/runs/run-1/cancel", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withUser(req, 5)

	w := httptest.NewRecorder()
	h.Cancel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ResolveGate ──────────────────────────────────────────────────────

func gateRequest(t *testing.T, runID string, userID int64, body map[string]any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/runs/"+runID+"/gate", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", runID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return withUser(req, userID)
}

func TestRunHandlers_ResolveGate_ApproverForbidden(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.runs["run-1"] = store.BundleRun{ID: "run-1", TriggeredBy: 5, Status: "running"}
	h := newTestRunHandlers(f)

	req := gateRequest(t, "run-1", 999, map[string]any{"node": "review", "approved": true})
	w := httptest.NewRecorder()
	h.ResolveGate(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunHandlers_ResolveGate_RunAlreadyFinished(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.runs["run-1"] = store.BundleRun{ID: "run-1", TriggeredBy: 5, Status: "finished"}
	h := newTestRunHandlers(f)

	req := gateRequest(t, "run-1", 5, map[string]any{"node": "review", "approved": true})
	w := httptest.NewRecorder()
	h.ResolveGate(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunHandlers_ResolveGate_AlreadyResolved(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.runs["run-1"] = store.BundleRun{ID: "run-1", TriggeredBy: 5, Status: "running"}
	h := newTestRunHandlers(f)

	req := gateRequest(t, "run-1", 5, map[string]any{"node": "review", "approved": true})
	w := httptest.NewRecorder()
	h.ResolveGate(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 (no pending gate), got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunHandlers_ResolveGate_Success(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.runs["run-1"] = store.BundleRun{ID: "run-1", TriggeredBy: 5, Status: "running"}
	f.addGate(1, "run-1", "review", "pending")
	h := newTestRunHandlers(f)

	waitCh := h.Engine.Gates.register(1)

	req := gateRequest(t, "run-1", 5, map[string]any{"node": "review", "approved": true})
	w := httptest.NewRecorder()
	h.ResolveGate(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if f.gates[1].Status != "approved" {
		t.Fatalf("expected gate row resolved to approved, got %+v", f.gates[1])
	}
	if len(f.audits) != 1 || f.audits[0].Action != "human_gate.approved" {
		t.Fatalf("expected an audit log row, got %+v", f.audits)
	}
	select {
	case res := <-waitCh:
		if !res.approved {
			t.Fatalf("expected approved resolution, got %+v", res)
		}
	default:
		t.Fatal("expected the registry channel to receive a resolution")
	}
}

// ── Stream ───────────────────────────────────────────────────────────

func decodeNDJSONLines(t *testing.T, body []byte) []runEventDTO {
	t.Helper()
	var out []runEventDTO
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var dto runEventDTO
		if err := json.Unmarshal(line, &dto); err != nil {
			t.Fatalf("decode NDJSON line %q: %v", line, err)
		}
		out = append(out, dto)
	}
	return out
}

func streamRequest(runID string, userID int64, afterID string) *http.Request {
	url := "/runs/" + runID + "/stream"
	if afterID != "" {
		url += "?after_id=" + afterID
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", runID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return withUser(req, userID)
}

// A terminal run (status finished/failed at connect time) replays history
// and closes immediately — spec-12's "遇 bundle.finished/failed...主动关闭"
// — without needing to wait out a poll interval, so this exercises the
// full history-replay path without any test-only timing hacks.
func TestRunHandlers_Stream_ReplaysHistoryAndClosesOnTerminalStatus(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 5, BundleRef: "b1", Version: "v1", DisplayMeta: []byte("{}")}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: "finished"}
	f.events = []store.BundleRunEvent{
		{ID: 1, RunID: "run-1", Type: "node.start", Payload: []byte("{}")},
		{ID: 2, RunID: "run-1", Type: "bundle.finished", Payload: []byte("{}")},
	}
	h := newTestRunHandlers(f)

	w := httptest.NewRecorder()
	h.Stream(w, streamRequest("run-1", 5, ""))

	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("expected NDJSON content type, got %q", ct)
	}
	if w.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatal("expected X-Accel-Buffering: no (required so nginx doesn't buffer the stream)")
	}
	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 {
		t.Fatalf("expected both history events replayed in order, got %+v", events)
	}
}

func TestRunHandlers_Stream_AfterIDResumesWithoutReplay(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 5, BundleRef: "b1", Version: "v1", DisplayMeta: []byte("{}")}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: "finished"}
	f.events = []store.BundleRunEvent{
		{ID: 1, RunID: "run-1", Type: "node.start", Payload: []byte("{}")},
		{ID: 2, RunID: "run-1", Type: "bundle.finished", Payload: []byte("{}")},
	}
	h := newTestRunHandlers(f)

	w := httptest.NewRecorder()
	h.Stream(w, streamRequest("run-1", 5, "1"))

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("expected only the event after id=1 to be resent, got %+v", events)
	}
}

func TestRunHandlers_Stream_FiltersInternalEventsForSubscriber(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 99, BundleRef: "b1", Version: "v1", DisplayMeta: []byte("{}")}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 30, Status: "finished"}
	f.events = []store.BundleRunEvent{
		{ID: 1, RunID: "run-1", Type: "tool.call", Payload: []byte("{}"), IsInternal: true},
		{ID: 2, RunID: "run-1", Type: "bundle.finished", Payload: []byte("{}"), IsInternal: false},
	}
	h := newTestRunHandlers(f)

	w := httptest.NewRecorder()
	h.Stream(w, streamRequest("run-1", 30, ""))
	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("expected only the external event visible to a subscriber, got %+v", events)
	}
}

func TestRunHandlers_Stream_OwnerSeesInternalEvents(t *testing.T) {
	f := newFakeRunHandlersQuerier()
	f.bundles[1] = store.Bundle{ID: 1, OwnerUserID: 99, BundleRef: "b1", Version: "v1", DisplayMeta: []byte("{}")}
	f.runs["run-1"] = store.BundleRun{ID: "run-1", BundleID: 1, TriggeredBy: 99, Status: "finished"}
	f.events = []store.BundleRunEvent{
		{ID: 1, RunID: "run-1", Type: "tool.call", Payload: []byte("{}"), IsInternal: true},
		{ID: 2, RunID: "run-1", Type: "bundle.finished", Payload: []byte("{}"), IsInternal: false},
	}
	h := newTestRunHandlers(f)

	w := httptest.NewRecorder()
	h.Stream(w, streamRequest("run-1", 99, ""))
	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 2 {
		t.Fatalf("expected the Bundle owner to see internal events too, got %+v", events)
	}
}
