package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/runstream"
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

func (s *stubRunRepo) ListPage(context.Context, run.ListQuery) ([]run.Run, error) { return nil, nil }
func (s *stubRunRepo) ListInSession(_ context.Context, triggeredBy int64, sessionID string) ([]run.Run, error) {
	var out []run.Run
	for _, r := range s.runs {
		if r.TriggeredBy == triggeredBy && r.SessionID == sessionID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *stubRunRepo) ListConversations(context.Context, int64, int64, int) ([]run.Conversation, error) {
	return nil, nil
}
func (s *stubRunRepo) UpdateStatus(context.Context, string, run.Status, string) error { return nil }
func (s *stubRunRepo) MarkCancelRequested(context.Context, string) error              { return nil }
func (s *stubRunRepo) AddUsage(context.Context, string, int64, float64) error         { return nil }

// 带锁：流式用例里 handler 在一个 goroutine 里读，引擎那侧在另一个
// goroutine 里追加，和线上是同一种并发形态（线上那边由 Postgres 兜住）。
type stubEventStore struct {
	mu     sync.Mutex
	events []run.Event
}

func (s *stubEventStore) append(evs ...run.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
}

func (s *stubEventStore) Append(_ context.Context, ev run.Event) (run.Event, error) {
	s.append(ev)
	return ev, nil
}

func (s *stubEventStore) ListAfter(_ context.Context, runID string, afterID int64) ([]run.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []run.Event
	for _, ev := range s.events {
		if ev.RunID != runID || ev.ID <= afterID {
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

// A nil Execution is not a valid Orchestrator result — the service launches
// whatever Prepare returns in its own goroutine, so returning nil panics as
// soon as a test actually reaches a launch.
type stubExecution struct{}

func (stubExecution) Start(int64, string, map[string]any, run.Limits) {}

func (stubOrchestrator) Prepare(context.Context, string, run.ResolvedBundle, map[string]run.GateConfig) (run.Execution, error) {
	return stubExecution{}, nil
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

func (stubIDs) NewRunID() (string, error)     { return "run-0000000000000001", nil }
func (stubIDs) NewSessionID() (string, error) { return "sess-0000000000000001", nil }

type runFixture struct {
	handlers *RunHandlers
	runs     *stubRunRepo
	events   *stubEventStore
	resolver *stubResolver
	bus      *runstream.Broker
}

func newRunFixture() *runFixture {
	runs := &stubRunRepo{runs: map[string]run.Run{}}
	events := &stubEventStore{}
	resolver := &stubResolver{}
	bus := runstream.NewBroker()
	svc := run.NewService(runs, events, resolver, stubDeps{}, stubOrchestrator{}, stubGates{}, stubNotifier{}, stubAudit{}, stubIDs{})
	return &runFixture{handlers: NewRunHandlers(svc, bus), runs: runs, events: events, resolver: resolver, bus: bus}
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

// 订阅者（不是这个 Bundle 的作者，userID 30 ≠ owner）拿到的是完整事件流，
// 包括 node.thinking。这曾经是被 is_internal 挡掉的那一批——挡掉的直接后果
// 是非作者身份的运行完全没有流式输出，只在最后蹦出一整段答案。
func TestStream_SubscriberReceivesStreamingEvents(t *testing.T) {
	f := newRunFixture()
	finishedRunWithEvents(f, 30, 99, []run.Event{
		{ID: 1, RunID: "run-1", Type: "node.thinking", Node: "writer", Payload: map[string]any{"text": "你"}},
		{ID: 2, RunID: "run-1", Type: "node.thinking", Node: "writer", Payload: map[string]any{"text": "好"}},
		{ID: 3, RunID: "run-1", Type: run.EventBundleFinished},
	})

	w := httptest.NewRecorder()
	f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream", "run-1", 30, nil))

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 3 {
		t.Fatalf("订阅者应当收到完整事件流（含 node.thinking），got %+v", events)
	}
	if events[0].Type != "node.thinking" || events[1].Type != "node.thinking" {
		t.Fatalf("前两条应当是流式增量，got %+v", events)
	}
}

// 实时投递：事件从广播器推过来，处理器不必等轮询、也不查库就写出去。
//
// 这就是"没有流式输出、都是按块输出"的另一半修复。原本 SSE 每 300ms 回头
// 查一次库，模型吐得再快，前端也只能每 300ms 收到一坨；现在事件一落库就
// 推到连接上。stubEventStore 里**没有**这几条事件——它们只存在于广播器里，
// 所以这个用例但凡能通过，就说明走的是实时那条路，不是轮询兜底。
func TestStream_DeliversLiveEventsFromTheBrokerWithoutPolling(t *testing.T) {
	f := newRunFixture()
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 5}
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.handlers.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream", "run-1", 5, nil))
	}()

	// 等处理器挂上订阅再推，否则事件会推给一个还不存在的订阅者。
	waitForSubscriber(t, f.bus, "run-1")
	f.bus.Publish(run.Event{ID: 1, RunID: "run-1", Type: "node.thinking", Node: "writer", Payload: map[string]any{"text": "你"}})
	f.bus.Publish(run.Event{ID: 2, RunID: "run-1", Type: "node.thinking", Node: "writer", Payload: map[string]any{"text": "好"}})
	f.bus.Publish(run.Event{ID: 3, RunID: "run-1", Type: run.EventBundleFinished})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("终态事件推过去之后流应当收线")
	}

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 3 {
		t.Fatalf("期望三条实时事件，实际 %d 条：%+v", len(events), events)
	}
	if events[0].Type != "node.thinking" || events[2].Type != run.EventBundleFinished {
		t.Fatalf("事件顺序不对：%+v", events)
	}
}

func waitForSubscriber(t *testing.T, b *runstream.Broker, runID string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if b.SubscriberCount(runID) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("处理器一直没挂上订阅")
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

// ── 草稿试运行 ────────────────────────────────────────────────────────

type stubAgentTestBundles struct{}

func (stubAgentTestBundles) Ensure(context.Context, int64) (int64, string, string, error) {
	return 77, "__agent_test__", "1.0", nil
}

func newAgentTestFixture() *runFixture {
	f := newRunFixture()
	// The service is shared with the fixture's handlers, so enabling the
	// surface here enables it for the handler under test too.
	f.handlers.svc.WithAgentTestRuns(stubAgentTestBundles{})
	return f
}

func TestCreateAgentTest_StartsARunFromAnInlineDefinition(t *testing.T) {
	f := newAgentTestFixture()
	body, _ := json.Marshal(createAgentTestRunRequest{
		Definition: map[string]any{"agent": "researcher", "role": "研究员"},
		Input:      map[string]any{"message": "hi"},
	})
	r := runRequest(http.MethodPost, "/api/v1/runs/agent-test", "", 1, body)
	w := httptest.NewRecorder()

	f.handlers.CreateAgentTest(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if len(f.runs.runs) != 1 {
		t.Fatalf("expected exactly one run to be created, got %d", len(f.runs.runs))
	}
}

func TestCreateAgentTest_RequiresAuth(t *testing.T) {
	f := newAgentTestFixture()
	body, _ := json.Marshal(createAgentTestRunRequest{Definition: map[string]any{"agent": "researcher"}})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runs/agent-test", bytes.NewReader(body))
	w := httptest.NewRecorder()

	f.handlers.CreateAgentTest(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCreateAgentTest_MalformedBodyReturns400(t *testing.T) {
	f := newAgentTestFixture()
	r := runRequest(http.MethodPost, "/api/v1/runs/agent-test", "", 1, []byte("{not json"))
	w := httptest.NewRecorder()

	f.handlers.CreateAgentTest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// racingRunRepo 复现 engine.finish 的真实时序：运行先是 running，等到某一
// 次状态查询时才翻成 finished——**而 bundle.finished 事件在翻状态之前就已
// 经落库**（finish 就是这个顺序）。
type racingRunRepo struct {
	*stubRunRepo
	events    *stubEventStore
	getCalls  int
	flipAfter int
}

func (s *racingRunRepo) Get(ctx context.Context, runID string) (run.Run, error) {
	s.getCalls++
	if s.getCalls >= s.flipAfter {
		// 事件先落库，再对外表现为终态——顺序同 engine.finish。
		if len(s.events.events) == 1 {
			s.events.append(
				run.Event{ID: 2, RunID: runID, Type: run.EventBundleFinished})
		}
		r := s.runs[runID]
		r.Status = run.StatusFinished
		s.runs[runID] = r
	}
	return s.stubRunRepo.Get(ctx, runID)
}

// 流处理器必须先读状态再读事件。反过来的话，两次读取之间落库的
// bundle.finished 就会被漏掉：处理器看见终态直接收线，而那一行永远没发出
// 去，前端只能判成"意外断流"——试运行面板于是一直卡在"运行中"，输入框跟着
// 锁死。这是"试运行一下就中断、后面再也发不出去"的根因。
func TestStream_DoesNotLoseTerminalEventWrittenJustBeforeStatusFlip(t *testing.T) {
	f := newRunFixture()
	finishedRunWithEvents(f, 5, 5, []run.Event{{ID: 1, RunID: "run-1", Type: "node.start", Node: "writer"}})
	// finishedRunWithEvents 把运行直接设成终态，这里退回运行中，让它在流的
	// 第二次轮询时才结束。
	r := f.runs.runs["run-1"]
	r.Status = run.StatusRunning
	f.runs.runs["run-1"] = r

	racing := &racingRunRepo{stubRunRepo: f.runs, events: f.events, flipAfter: 2}
	svc := run.NewService(racing, f.events, f.resolver, stubDeps{}, stubOrchestrator{}, stubGates{}, stubNotifier{}, stubAudit{}, stubIDs{})
	// 这个用例故意不挂广播器：它模拟的是"事件绕过推流直接落库"（另一个
	// 副本在跑，或者广播器漏推），走的正是兜底轮询那条路。
	h := NewRunHandlers(svc, nil)

	w := httptest.NewRecorder()
	h.Stream(w, runRequest(http.MethodGet, "/runs/run-1/stream", "run-1", 5, nil))

	events := decodeNDJSONLines(t, w.Body.Bytes())
	if len(events) != 2 {
		t.Fatalf("期望两条事件（含终态那条），实际 %d 条：%+v", len(events), events)
	}
	if events[1].Type != run.EventBundleFinished {
		t.Fatalf("最后一条应当是 %s，实际是 %s——终态事件被漏掉了", run.EventBundleFinished, events[1].Type)
	}
}
