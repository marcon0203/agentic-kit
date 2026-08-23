package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// The run rules — the launch chain, who may approve a gate, what a
// subscriber may see — are tested against the service in
// internal/domain/run. What is left here is transport, and above all the
// stream: NDJSON framing, the headers nginx needs, resume-by-after_id, and
// closing on a terminal run.

type stubRunRepo struct {
	runs map[string]run.Run
}

func (s *stubRunRepo) Create(_ context.Context, r run.Run) (run.Run, error) {
	r.CreatedAt = time.Now()
	s.runs[r.ID] = r
	return r, nil
}

func (s *stubRunRepo) Get(_ context.Context, runID string) (run.Run, error) {
	r, ok := s.runs[runID]
	if !ok {
		return run.Run{}, run.ErrNotFound
	}
	return r, nil
}

func (s *stubRunRepo) ListPage(context.Context, run.ListQuery) ([]run.Run, error)     { return nil, nil }
func (s *stubRunRepo) UpdateStatus(context.Context, string, run.Status, string) error { return nil }
func (s *stubRunRepo) MarkCancelRequested(context.Context, string) error              { return nil }
func (s *stubRunRepo) AddUsage(context.Context, string, int64, float64) error         { return nil }

type stubEventStore struct{ events []run.Event }

func (s *stubEventStore) Append(_ context.Context, ev run.Event) error {
	s.events = append(s.events, ev)
	return nil
}

func (s *stubEventStore) ListAfter(_ context.Context, runID string, afterID int64, includeInternal bool) ([]run.Event, error) {
	var out []run.Event
	for _, ev := range s.events {
		if ev.RunID != runID || ev.ID <= afterID {
			continue
		}
		if ev.IsInternal && !includeInternal {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

type stubResolver struct{ bundle run.ResolvedBundle }

func (s *stubResolver) Resolve(context.Context, int64, string, string) (run.ResolvedBundle, error) {
	return s.bundle, nil
}

func (s *stubResolver) LoadForRun(context.Context, int64) (run.ResolvedBundle, error) {
	return s.bundle, nil
}

type stubDeps struct{}

func (stubDeps) Check(context.Context, int64, map[string]any) (run.DependencyStatus, error) {
	return run.DependenciesOK, nil
}

type stubOrchestrator struct{}

func (stubOrchestrator) Prepare(context.Context, string, run.ResolvedBundle, map[string]run.GateConfig) (run.Execution, error) {
	return nil, nil
}
func (stubOrchestrator) Cancel(string) bool { return true }

type stubGates struct{}

func (stubGates) CreatePending(context.Context, string, run.GateConfig) (run.Gate, error) {
	return run.Gate{}, nil
}
func (stubGates) FindPending(context.Context, string, string) (run.Gate, error) {
	return run.Gate{}, run.ErrNotFound
}
func (stubGates) Resolve(context.Context, int64, run.Decision, *int64) error { return nil }
func (stubGates) ListPastTimeout(context.Context) ([]run.Gate, error)        { return nil, nil }

type stubNotifier struct{}

func (stubNotifier) Notify(int64, run.Decision) bool { return true }

type stubAudit struct{}

func (stubAudit) Record(context.Context, *int64, string, string, string, map[string]any) error {
	return nil
}

type stubIDs struct{}

func (stubIDs) NewRunID() (string, error) { return "run-0000000000000001", nil }

type runFixture struct {
	handlers *RunHandlers
	runs     *stubRunRepo
	events   *stubEventStore
	resolver *stubResolver
}

func newRunFixture() *runFixture {
	runs := &stubRunRepo{runs: map[string]run.Run{}}
	events := &stubEventStore{}
	resolver := &stubResolver{}
	svc := run.NewService(runs, events, resolver, stubDeps{}, stubOrchestrator{}, stubGates{}, stubNotifier{}, stubAudit{}, stubIDs{})
	return &runFixture{handlers: NewRunHandlers(svc), runs: runs, events: events, resolver: resolver}
}

func runRequest(method, url, runID string, userID int64, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", runID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return r.WithContext(WithUserID(r.Context(), userID))
}

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

// finishedRunWithEvents sets up a terminal run, which lets the stream tests
// exercise the full replay path and then close without waiting out a poll
// interval — no test-only timing hooks needed.
func finishedRunWithEvents(f *runFixture, triggeredBy, ownerID int64, events []run.Event) {
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: triggeredBy, Status: run.StatusFinished}
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, Ref: "b1", Version: "v1", OwnerUserID: ownerID}
	f.events.events = events
}

func TestStream_ReplaysHistoryAndClosesOnTerminalStatus(t *testing.T) {
	f := newRunFixture()
	finishedRunWithEvents(f, 5, 5, []run.Event{
		{ID: 1, RunID: "run-1", Type: "node.start", Node: "writer"},
		{ID: 2, RunID: "run-1", Type: run.EventBundleFinished},
	})

	w := httptest.NewRecorder()
	f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream", "run-1", 5, nil))

	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content type = %q, want application/x-ndjson", ct)
	}
	if w.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatal("X-Accel-Buffering: no is required, or nginx buffers the whole stream to its end")
	}
	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 {
		t.Fatalf("expected both events replayed in order, got %+v", events)
	}
	if events[0].Node == nil || *events[0].Node != "writer" {
		t.Fatalf("node should be carried through: %+v", events[0])
	}
	if events[1].Node != nil {
		t.Fatal("a run-level event has no node, and must omit the field rather than send null")
	}
}

func TestStream_AfterIDResumesWithoutReplay(t *testing.T) {
	f := newRunFixture()
	finishedRunWithEvents(f, 5, 5, []run.Event{
		{ID: 1, RunID: "run-1", Type: "node.start"},
		{ID: 2, RunID: "run-1", Type: run.EventBundleFinished},
	})

	w := httptest.NewRecorder()
	f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream?after_id=1", "run-1", 5, nil))

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("expected only the event after id=1, got %+v", events)
	}
}

func TestStream_FiltersInternalEventsForSubscriber(t *testing.T) {
	f := newRunFixture()
	finishedRunWithEvents(f, 30, 99, []run.Event{
		{ID: 1, RunID: "run-1", Type: "tool.call", IsInternal: true},
		{ID: 2, RunID: "run-1", Type: run.EventBundleFinished},
	})

	w := httptest.NewRecorder()
	f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream", "run-1", 30, nil))

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("a subscriber must not receive internal events, got %+v", events)
	}
}

// A stream that cannot start must fail as a normal envelope, before any
// header is written — a 200 with an error line inside it would be
// indistinguishable from a run that produced nothing.
func TestStream_UnknownRunFailsAsAnEnvelope(t *testing.T) {
	f := newRunFixture()

	w := httptest.NewRecorder()
	f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/nope/stream", "nope", 5, nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/x-ndjson" {
		t.Fatal("a failed stream must not claim to be NDJSON")
	}
	if !containsCode(w.Body.String(), ErrRunNotFound) {
		t.Fatalf("body should carry ErrRunNotFound: %s", w.Body.String())
	}
}

func TestGet_ResponseShape(t *testing.T) {
	f := newRunFixture()
	finished := time.Now()
	created := finished.Add(-90 * time.Second)
	f.runs.runs["run-1"] = run.Run{
		ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusFinished,
		SharedState: map[string]any{"final_answer": "42"},
		Usage:       run.Usage{TotalTokens: 1200, CostUSD: 0.42},
		CreatedAt:   created, FinishedAt: &finished,
	}
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, Ref: "content-pipeline", Version: "2.1", OwnerUserID: 5}

	w := httptest.NewRecorder()
	f.handlers.Get(w, runRequest(http.MethodGet, "/runs/run-1", "run-1", 5, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var dto runDetailDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if dto.RunID != "run-1" || dto.BundleRef != "content-pipeline" || dto.BundleVersion != "2.1" {
		t.Fatalf("unexpected summary: %+v", dto.runSummaryDTO)
	}
	if !dto.IsOwner || dto.SharedState["final_answer"] != "42" {
		t.Fatalf("unexpected detail: %+v", dto)
	}
	if dto.Usage.TotalTokens != 1200 || dto.Usage.CostUSD != 0.42 || dto.Usage.DurationSeconds != 90 {
		t.Fatalf("usage = %+v, want duration derived from created/finished", dto.Usage)
	}
	if dto.Error != nil {
		t.Fatalf("a successful run must send error: null, got %q", *dto.Error)
	}
}

func TestGet_FailedRunCarriesItsError(t *testing.T) {
	f := newRunFixture()
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusFailed, Error: run.FailGeneric}
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 5}

	w := httptest.NewRecorder()
	f.handlers.Get(w, runRequest(http.MethodGet, "/runs/run-1", "run-1", 5, nil))

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var dto runDetailDTO
	_ = json.Unmarshal(dataBytes, &dto)
	if dto.Error == nil || *dto.Error != run.FailGeneric {
		t.Fatalf("expected the sanitised failure message, got %+v", dto.Error)
	}
}

func TestCancel_FinishedRunReturns409(t *testing.T) {
	f := newRunFixture()
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusFinished}

	w := httptest.NewRecorder()
	f.handlers.Cancel(w, runRequest(http.MethodPost, "/runs/run-1/cancel", "run-1", 5, nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestCancel_RunningRunReturns204(t *testing.T) {
	f := newRunFixture()
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}

	w := httptest.NewRecorder()
	f.handlers.Cancel(w, runRequest(http.MethodPost, "/runs/run-1/cancel", "run-1", 5, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 must have an empty body, got %q", w.Body.String())
	}
}

func TestResolveGate_MalformedBodyReturns400(t *testing.T) {
	f := newRunFixture()
	f.runs.runs["run-1"] = run.Run{ID: "run-1", TriggeredBy: 5, Status: run.StatusRunning}

	w := httptest.NewRecorder()
	f.handlers.ResolveGate(w, runRequest(http.MethodPost, "/runs/run-1/gate", "run-1", 5, []byte("{not json")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestRunHandlers_RequireAuthenticatedUser(t *testing.T) {
	f := newRunFixture()
	for name, handler := range map[string]http.HandlerFunc{
		"create": f.handlers.Create, "list": f.handlers.List, "get": f.handlers.Get,
		"stream": f.handlers.Stream, "cancel": f.handlers.Cancel, "gate": f.handlers.ResolveGate,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/runs/run-1", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a user: status = %d, want 401", name, w.Code)
		}
	}
}

func TestList_InvalidCursorReturns400(t *testing.T) {
	f := newRunFixture()
	w := httptest.NewRecorder()
	f.handlers.List(w, runRequest(http.MethodGet, "/runs?cursor=!!!not-base64!!!", "", 5, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
