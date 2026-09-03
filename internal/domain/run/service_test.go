package run_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeRepo struct {
	runs    map[string]run.Run
	created []run.Run
	list    []run.Run
	lastQ   run.ListQuery

	cancelRequested map[string]bool
	statuses        map[string]run.Status
	usage           run.Usage
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{runs: map[string]run.Run{}, cancelRequested: map[string]bool{}, statuses: map[string]run.Status{}}
}

func (f *fakeRepo) Create(_ context.Context, r run.Run) (run.Run, error) {
	r.CreatedAt = time.Now()
	f.runs[r.ID] = r
	f.created = append(f.created, r)
	return r, nil
}

func (f *fakeRepo) Get(_ context.Context, runID string) (run.Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return run.Run{}, run.ErrNotFound
	}
	return r, nil
}

func (f *fakeRepo) ListPage(_ context.Context, q run.ListQuery) ([]run.Run, error) {
	f.lastQ = q
	if q.Offset >= len(f.list) {
		return nil, nil
	}
	rows := f.list[q.Offset:]
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

func (f *fakeRepo) ListInSession(_ context.Context, triggeredBy int64, sessionID string) ([]run.Run, error) {
	var out []run.Run
	for _, r := range f.list {
		if r.TriggeredBy == triggeredBy && r.SessionID == sessionID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateStatus(_ context.Context, runID string, status run.Status, _ string) error {
	f.statuses[runID] = status
	return nil
}

func (f *fakeRepo) MarkCancelRequested(_ context.Context, runID string) error {
	f.cancelRequested[runID] = true
	return nil
}

func (f *fakeRepo) AddUsage(_ context.Context, _ string, tokens int64, cost float64) error {
	f.usage.TotalTokens += tokens
	f.usage.CostUSD += cost
	return nil
}

type fakeEvents struct{ appended []run.Event }

func (f *fakeEvents) Append(_ context.Context, ev run.Event) error {
	f.appended = append(f.appended, ev)
	return nil
}

func (f *fakeEvents) ListAfter(_ context.Context, runID string, afterID int64, includeInternal bool) ([]run.Event, error) {
	var out []run.Event
	for _, ev := range f.appended {
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

type fakeResolver struct {
	bundle     run.ResolvedBundle
	resolved   run.ResolvedBundle
	resolveErr error
}

func (f *fakeResolver) Resolve(_ context.Context, _ int64, _, _ string) (run.ResolvedBundle, error) {
	if f.resolveErr != nil {
		return run.ResolvedBundle{}, f.resolveErr
	}
	return f.resolved, nil
}

func (f *fakeResolver) LoadForRun(_ context.Context, _ int64) (run.ResolvedBundle, error) {
	return f.bundle, nil
}

type fakeDeps struct {
	status run.DependencyStatus
	err    error
	// missing 让这个 fake 同时实现 run.ProviderDetailChecker，用来盯住
	// "试运行的报错要说清是哪个提供商没配"。
	missing []string
}

func (f *fakeDeps) Check(context.Context, int64, map[string]any) (run.DependencyStatus, error) {
	return f.status, f.err
}

func (f *fakeDeps) MissingProviders(context.Context, int64, map[string]any) ([]string, error) {
	return f.missing, nil
}

type fakeOrchestrator struct {
	prepareErr error
	prepared   int
	// preparedBundle is the last ResolvedBundle handed to Prepare — what a
	// 草稿试运行 test asserts on, since the whole point there is *what* got
	// compiled, not just that something did.
	preparedBundle run.ResolvedBundle
	// sessionID 是最后一次 Start 收到的会话 id——"连着发消息共享同一段对
	// 话"这件事就是在这里断言的。
	sessionID   string
	started     chan struct{}
	cancelled   []string
	gateConfigs map[string]run.GateConfig
	limits      run.Limits
}

func (f *fakeOrchestrator) Prepare(_ context.Context, _ string, b run.ResolvedBundle, gates map[string]run.GateConfig) (run.Execution, error) {
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	f.prepared++
	f.preparedBundle = b
	f.gateConfigs = gates
	return &fakeExecution{owner: f}, nil
}

func (f *fakeOrchestrator) Cancel(runID string) bool {
	f.cancelled = append(f.cancelled, runID)
	return true
}

type fakeExecution struct{ owner *fakeOrchestrator }

func (x *fakeExecution) Start(_ int64, sessionID string, _ map[string]any, limits run.Limits) {
	x.owner.limits = limits
	x.owner.sessionID = sessionID
	if x.owner.started != nil {
		close(x.owner.started)
	}
}

type fakeGates struct {
	pending    map[string]run.Gate
	resolved   map[int64]run.Decision
	byID       map[int64]run.Gate
	timedOut   []run.Gate
	resolveErr error
}

func newFakeGates() *fakeGates {
	return &fakeGates{pending: map[string]run.Gate{}, resolved: map[int64]run.Decision{}, byID: map[int64]run.Gate{}}
}

func (f *fakeGates) CreatePending(_ context.Context, runID string, cfg run.GateConfig) (run.Gate, error) {
	g := run.Gate{ID: int64(len(f.byID) + 1), RunID: runID, Node: cfg.Node, Status: run.GateStatusPending, OnTimeout: cfg.OnTimeout}
	f.byID[g.ID] = g
	f.pending[runID+"/"+cfg.Node] = g
	return g, nil
}

func (f *fakeGates) FindPending(_ context.Context, runID, node string) (run.Gate, error) {
	g, ok := f.pending[runID+"/"+node]
	if !ok {
		return run.Gate{}, run.ErrNotFound
	}
	return g, nil
}

func (f *fakeGates) Resolve(_ context.Context, gateID int64, d run.Decision, _ *int64) error {
	if f.resolveErr != nil {
		return f.resolveErr
	}
	f.resolved[gateID] = d
	return nil
}

func (f *fakeGates) ListPastTimeout(context.Context) ([]run.Gate, error) { return f.timedOut, nil }

type fakeNotifier struct{ notified map[int64]run.Decision }

func (f *fakeNotifier) Notify(gateID int64, d run.Decision) bool {
	if f.notified == nil {
		f.notified = map[int64]run.Decision{}
	}
	f.notified[gateID] = d
	return true
}

type auditEntry struct {
	action string
	actor  *int64
	detail map[string]any
}

type fakeAudit struct{ entries []auditEntry }

func (f *fakeAudit) Record(_ context.Context, actor *int64, action, _, _ string, detail map[string]any) error {
	f.entries = append(f.entries, auditEntry{action: action, actor: actor, detail: detail})
	return nil
}

type fixedIDs struct{ id string }

func (f fixedIDs) NewRunID() (string, error) { return f.id, nil }

// NewSessionID 固定值，好让"没传 session_id 就自己开一段"这条路径可断言。
func (fixedIDs) NewSessionID() (string, error) { return "sess-generated", nil }

// harness bundles every fake so a test can reach into whichever one it is
// making an assertion about.
type harness struct {
	svc      *run.Service
	repo     *fakeRepo
	events   *fakeEvents
	resolver *fakeResolver
	deps     *fakeDeps
	orch     *fakeOrchestrator
	gates    *fakeGates
	notifier *fakeNotifier
	audit    *fakeAudit
}

func newHarness() *harness {
	h := &harness{
		repo: newFakeRepo(), events: &fakeEvents{},
		resolver: &fakeResolver{}, deps: &fakeDeps{},
		orch: &fakeOrchestrator{}, gates: newFakeGates(),
		notifier: &fakeNotifier{}, audit: &fakeAudit{},
	}
	h.svc = run.NewService(h.repo, h.events, h.resolver, h.deps, h.orch, h.gates, h.notifier, h.audit, fixedIDs{id: "run-test0000000001"})
	return h
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

// ── Launch chain ─────────────────────────────────────────────────────

func TestStart_RequiresBundleRef(t *testing.T) {
	h := newHarness()
	_, err := h.svc.Start(context.Background(), 1, run.StartCommand{})

	de := mustDomainErr(t, err)
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 1 || de.Details[0].Field != "bundle_ref" {
		t.Fatalf("expected a bundle_ref field error, got %+v", de)
	}
}

func TestStart_NotSubscribedIsForbidden(t *testing.T) {
	h := newHarness()
	h.resolver.resolveErr = run.ErrNotSubscribed

	_, err := h.svc.Start(context.Background(), 1, run.StartCommand{BundleRef: "someone-elses"})
	de := mustDomainErr(t, err)
	if de.Kind != domain.KindForbidden || de.Code != domain.CodeNotSubscribed {
		t.Fatalf("expected 403/70003, got kind=%v code=%d", de.Kind, de.Code)
	}
}

// Each dependency verdict has its own code so the frontend can tell a
// missing model provider (fix your settings) from a disabled resource
// (ask the author) — while the *message* stays identical and generic,
// because naming the resource would leak a private Bundle's contents.
func TestStart_DependencyVerdictsMapToCodesButNotToNames(t *testing.T) {
	cases := map[run.DependencyStatus]int{
		run.DependencyAgentMissing:        domain.CodeAgentVersionNotFound,
		run.DependencyResourceUnavailable: domain.CodeResourceDisabled,
		run.DependencyProviderMissing:     domain.CodeProviderNotConfigured,
	}
	for status, wantCode := range cases {
		h := newHarness()
		h.resolver.resolved = run.ResolvedBundle{BundleID: 1, Ref: "b", Version: "1.0", Definition: map[string]any{}}
		h.deps.status = status

		_, err := h.svc.Start(context.Background(), 1, run.StartCommand{BundleRef: "b"})
		de := mustDomainErr(t, err)
		if de.Code != wantCode {
			t.Fatalf("status %v: code = %d, want %d", status, de.Code, wantCode)
		}
		if de.Kind != domain.KindUnprocessable {
			t.Fatalf("status %v: kind = %v, want unprocessable", status, de.Kind)
		}
		if len(h.repo.created) != 0 {
			t.Fatalf("status %v: a rejected launch must not leave a run row behind", status)
		}
	}
}

func TestStart_CompileFailureLeavesNoRun(t *testing.T) {
	h := newHarness()
	h.resolver.resolved = run.ResolvedBundle{BundleID: 1, Ref: "b", Version: "1.0", Definition: map[string]any{}}
	h.orch.prepareErr = errors.New("compile agent \"writer\": model provider unreachable")

	_, err := h.svc.Start(context.Background(), 1, run.StartCommand{BundleRef: "b"})
	de := mustDomainErr(t, err)
	if de.Kind != domain.KindUnprocessable || de.Code != domain.CodeAgentVersionNotFound {
		t.Fatalf("expected 422/40004, got kind=%v code=%d", de.Kind, de.Code)
	}
	if de.Message == err.Error() && len(h.repo.created) > 0 {
		t.Fatal("a failed compile must not create a run")
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("expected no run row, got %+v", h.repo.created)
	}
}

func TestStart_LaunchesAsynchronouslyWithParsedLimitsAndGates(t *testing.T) {
	h := newHarness()
	h.orch.started = make(chan struct{})
	h.resolver.resolved = run.ResolvedBundle{
		BundleID: 7, Ref: "content-pipeline", Version: "2.1",
		Definition: map[string]any{
			"limits": map[string]any{"max_total_tokens": float64(1000), "max_cost_usd": 2.5, "max_wall_clock_seconds": float64(600)},
			"orchestration": map[string]any{"human_gates": []any{
				map[string]any{"after": "review", "timeout_seconds": float64(300), "on_timeout": "auto_reject"},
			}},
		},
	}

	created, err := h.svc.Start(context.Background(), 42, run.StartCommand{BundleRef: "content-pipeline"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if created.ID != "run-test0000000001" || created.Status != run.StatusRunning {
		t.Fatalf("unexpected run: %+v", created)
	}
	if created.BundleRef != "content-pipeline" || created.BundleVersion != "2.1" {
		t.Fatalf("run should carry the resolved ref/version, got %+v", created)
	}

	select {
	case <-h.orch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected the execution to be started in its own goroutine")
	}

	if h.orch.limits != (run.Limits{MaxTotalTokens: 1000, MaxCostUSD: 2.5, MaxWallClockSeconds: 600}) {
		t.Fatalf("limits not passed through: %+v", h.orch.limits)
	}
	gate, ok := h.orch.gateConfigs["review"]
	if !ok || gate.OnTimeout != run.TimeoutAutoReject || gate.TimeoutSeconds == nil || *gate.TimeoutSeconds != 300 {
		t.Fatalf("gate config not parsed: %+v", h.orch.gateConfigs)
	}
}

// ── 草稿试运行 ────────────────────────────────────────────────────────

type fakeTestBundles struct {
	calls int
	err   error
}

func (f *fakeTestBundles) Ensure(context.Context, int64) (int64, string, string, error) {
	f.calls++
	if f.err != nil {
		return 0, "", "", f.err
	}
	return 77, "__agent_test__", "1.0", nil
}

func TestStartAgentTest_WrapsTheDraftInASingleBundleInline(t *testing.T) {
	h := newHarness()
	h.orch.started = make(chan struct{})
	h.svc.WithAgentTestRuns(&fakeTestBundles{})

	def := map[string]any{"agent": "researcher", "role": "研究员", "persona": "你是一名研究员"}
	created, err := h.svc.StartAgentTest(context.Background(), 42, run.AgentTestCommand{
		Definition: def, Input: map[string]any{"message": "hi"},
	})
	if err != nil {
		t.Fatalf("StartAgentTest: %v", err)
	}
	if created.BundleID != 77 || created.BundleRef != "__agent_test__" {
		t.Fatalf("the run should hang off the placeholder bundle, got %+v", created)
	}

	// The compiler must receive the draft inline — never a (ref, version)
	// pointing at a registry row that does not exist for an unsaved Agent.
	agents, _ := h.orch.preparedBundle.Definition["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("expected exactly one agent, got %+v", h.orch.preparedBundle.Definition)
	}
	entry, _ := agents[0].(map[string]any)
	if entry["ref"] != "researcher" {
		t.Fatalf("unexpected agent ref: %+v", entry)
	}
	inline, ok := entry["definition"].(map[string]any)
	if !ok || inline["persona"] != "你是一名研究员" {
		t.Fatalf("expected the draft definition inline, got %+v", entry)
	}
	if h.orch.preparedBundle.Definition["type"] != "single" {
		t.Fatalf("a one-agent test run should use the single run type, got %+v", h.orch.preparedBundle.Definition)
	}

	select {
	case <-h.orch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected the execution to be started in its own goroutine")
	}
}

func TestStartAgentTest_RequiresAnAgentRef(t *testing.T) {
	h := newHarness()
	h.svc.WithAgentTestRuns(&fakeTestBundles{})

	_, err := h.svc.StartAgentTest(context.Background(), 1, run.AgentTestCommand{
		Definition: map[string]any{"role": "研究员"},
	})
	de := mustDomainErr(t, err)
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 1 || de.Details[0].Field != "definition.agent" {
		t.Fatalf("unexpected error: %+v", de)
	}
}

func TestStartAgentTest_NotConfigured_ReturnsClearError(t *testing.T) {
	h := newHarness() // no WithAgentTestRuns

	_, err := h.svc.StartAgentTest(context.Background(), 1, run.AgentTestCommand{
		Definition: map[string]any{"agent": "researcher"},
	})
	if de := mustDomainErr(t, err); de.Code != domain.CodeValidationFailed {
		t.Fatalf("unexpected error: %+v", de)
	}
}

// A draft that references a disabled resource must be rejected by the same
// pre-flight a real run gets — the test panel is not a way around it.
func TestStartAgentTest_RunsTheSameDependencyPreflight(t *testing.T) {
	h := newHarness()
	h.svc.WithAgentTestRuns(&fakeTestBundles{})
	h.deps.status = run.DependencyResourceUnavailable

	_, err := h.svc.StartAgentTest(context.Background(), 1, run.AgentTestCommand{
		Definition: map[string]any{"agent": "researcher"},
	})
	if de := mustDomainErr(t, err); de.Code != domain.CodeResourceDisabled {
		t.Fatalf("unexpected error: %+v", de)
	}
	if len(h.repo.runs) != 0 {
		t.Fatal("a rejected test run must not leave a run row behind")
	}
}

// ── Black-box read rules ─────────────────────────────────────────────

func TestGet_SubscriberSeesOnlyDeclaredOutputs(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{
		ID: "run-1", BundleID: 1, TriggeredBy: 30, Status: run.StatusFinished,
		SharedState: map[string]any{"final_answer": "42", "internal_scratch": "secret prompt"},
	}
	h.resolver.bundle = run.ResolvedBundle{BundleID: 1, Ref: "b1", Version: "v1", OwnerUserID: 99, DeclaredOutputs: []string{"final_answer"}}

	detail, err := h.svc.Get(context.Background(), 30, "run-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.IsOwner {
		t.Fatal("expected is_owner=false so the frontend renders black-box chrome")
	}
	if _, leaked := detail.SharedState["internal_scratch"]; leaked {
		t.Fatalf("undeclared key leaked to a subscriber: %+v", detail.SharedState)
	}
	if detail.SharedState["final_answer"] != "42" {
		t.Fatalf("declared output missing: %+v", detail.SharedState)
	}
}

func TestGet_AuthorSeesEverything(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{
		ID: "run-1", BundleID: 1, TriggeredBy: 99, Status: run.StatusFinished,
		SharedState: map[string]any{"final_answer": "42", "internal_scratch": "secret prompt"},
	}
	h.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 99, DeclaredOutputs: []string{"final_answer"}}

	detail, err := h.svc.Get(context.Background(), 99, "run-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !detail.IsOwner || detail.SharedState["internal_scratch"] != "secret prompt" {
		t.Fatalf("the Bundle's author should see the whole shared_state: %+v", detail)
	}
}

// Someone else's run is not-found rather than forbidden: a 403 would
// confirm the id exists, which is itself something they should not learn.
func TestGet_SomeoneElsesRunIsNotFound(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", TriggeredBy: 5, Status: run.StatusFinished}

	_, err := h.svc.Get(context.Background(), 999, "run-1")
	de := mustDomainErr(t, err)
	if de.Kind != domain.KindNotFound || de.Code != domain.CodeRunNotFound {
		t.Fatalf("expected 404/50001, got kind=%v code=%d", de.Kind, de.Code)
	}
}

func TestEventsAfter_InternalEventsOnlyForAuthor(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 30, Status: run.StatusFinished}
	h.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 99}
	h.events.appended = []run.Event{
		{ID: 1, RunID: "run-1", Type: "tool.call", IsInternal: true},
		{ID: 2, RunID: "run-1", Type: run.EventBundleFinished},
	}

	subscriberView, err := h.svc.EventsAfter(context.Background(), 30, "run-1", 0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(subscriberView) != 1 || subscriberView[0].ID != 2 {
		t.Fatalf("a subscriber must not see internal events: %+v", subscriberView)
	}

	h.repo.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 99, Status: run.StatusFinished}
	authorView, err := h.svc.EventsAfter(context.Background(), 99, "run-1", 0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(authorView) != 2 {
		t.Fatalf("the author should see internal events too: %+v", authorView)
	}
}

// ── Cancel ───────────────────────────────────────────────────────────

func TestCancel_RunningRunIsStoppedAndRecorded(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}

	if err := h.svc.Cancel(context.Background(), 5, "run-1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !h.repo.cancelRequested["run-1"] {
		t.Fatal("cancel must be recorded in storage, so a restarted engine still sees the request")
	}
	if len(h.orch.cancelled) != 1 || h.orch.cancelled[0] != "run-1" {
		t.Fatalf("expected the orchestrator to be told to stop: %+v", h.orch.cancelled)
	}
}

func TestCancel_FinishedRunConflicts(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusFinished}

	de := mustDomainErr(t, h.svc.Cancel(context.Background(), 5, "run-1"))
	if de.Kind != domain.KindConflict || de.Code != domain.CodeRunAlreadyFinished {
		t.Fatalf("expected 409/50002, got kind=%v code=%d", de.Kind, de.Code)
	}
	if h.repo.cancelRequested["run-1"] {
		t.Fatal("a finished run must not be marked cancel-requested")
	}
}

// ── Gates ────────────────────────────────────────────────────────────

func runningRunWithGate(h *harness) {
	h.repo.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}
	_, _ = h.gates.CreatePending(context.Background(), "run-1", run.GateConfig{Node: "review", OnTimeout: run.TimeoutAbort})
}

// V1 has no role system, so "the approver role" is exactly the user who
// triggered the run — anyone else is refused even if they can see the run.
func TestResolveGate_OnlyTheTriggeringUserMayApprove(t *testing.T) {
	h := newHarness()
	runningRunWithGate(h)

	de := mustDomainErr(t, h.svc.ResolveGate(context.Background(), 999, "run-1", run.ResolveGateCommand{Node: "review", Approved: true}))
	if de.Kind != domain.KindForbidden || de.Code != domain.CodeGateApproverForbidden {
		t.Fatalf("expected 403/50004, got kind=%v code=%d", de.Kind, de.Code)
	}
	if len(h.gates.resolved) != 0 {
		t.Fatal("an unauthorised decision must not touch the gate")
	}
}

func TestResolveGate_RequiresNode(t *testing.T) {
	h := newHarness()
	runningRunWithGate(h)

	de := mustDomainErr(t, h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Approved: true}))
	if de.Code != domain.CodeValidationFailed {
		t.Fatalf("expected 10001, got %d", de.Code)
	}
}

func TestResolveGate_FinishedRunConflicts(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", TriggeredBy: 5, Status: run.StatusFinished}

	de := mustDomainErr(t, h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Node: "review", Approved: true}))
	if de.Code != domain.CodeRunAlreadyFinished {
		t.Fatalf("expected 50002, got %d", de.Code)
	}
}

func TestResolveGate_NoPendingGateConflicts(t *testing.T) {
	h := newHarness()
	h.repo.runs["run-1"] = run.Run{ID: "run-1", TriggeredBy: 5, Status: run.StatusRunning}

	de := mustDomainErr(t, h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Node: "review", Approved: true}))
	if de.Kind != domain.KindConflict || de.Code != domain.CodeGateAlreadyResolved {
		t.Fatalf("expected 409/50003, got kind=%v code=%d", de.Kind, de.Code)
	}
}

// Losing a race with the timeout scanner must read as "already handled",
// not as a server error and not as a silent overwrite of the winner.
func TestResolveGate_LostRaceConflicts(t *testing.T) {
	h := newHarness()
	runningRunWithGate(h)
	h.gates.resolveErr = run.ErrGateResolved

	de := mustDomainErr(t, h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Node: "review", Approved: true}))
	if de.Code != domain.CodeGateAlreadyResolved {
		t.Fatalf("expected 50003, got %d", de.Code)
	}
	if len(h.notifier.notified) != 0 {
		t.Fatal("a lost race must not unblock the run a second time")
	}
}

func TestResolveGate_ApprovalNotifiesAuditsAndEmitsEvent(t *testing.T) {
	h := newHarness()
	runningRunWithGate(h)

	if err := h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Node: "review", Approved: true}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if d := h.gates.resolved[1]; d.Status != run.GateStatusApproved || !d.Approved {
		t.Fatalf("gate not approved: %+v", d)
	}
	if d, ok := h.notifier.notified[1]; !ok || !d.Approved {
		t.Fatalf("the waiting run was not unblocked: %+v", h.notifier.notified)
	}
	if len(h.audit.entries) != 1 || h.audit.entries[0].action != "human_gate.approved" {
		t.Fatalf("expected one audit entry: %+v", h.audit.entries)
	}
	if h.audit.entries[0].actor == nil || *h.audit.entries[0].actor != 5 {
		t.Fatalf("audit entry should name the approver: %+v", h.audit.entries[0])
	}

	var resolvedEvent *run.Event
	for i := range h.events.appended {
		if h.events.appended[i].Type == run.EventGateResolved {
			resolvedEvent = &h.events.appended[i]
		}
	}
	if resolvedEvent == nil || resolvedEvent.Node != "review" {
		t.Fatalf("expected a human_gate.resolved event: %+v", h.events.appended)
	}
	if resolvedEvent.IsInternal {
		t.Fatal("a gate resolution is what the Chat gate card renders — it must not be internal")
	}
}

// A rejection carries a reason because it becomes the blocked branch's
// error; the comment, when given, is that reason.
func TestResolveGate_RejectionCarriesTheComment(t *testing.T) {
	h := newHarness()
	runningRunWithGate(h)

	if err := h.svc.ResolveGate(context.Background(), 5, "run-1", run.ResolveGateCommand{Node: "review", Comment: "预算不足"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d := h.gates.resolved[1]
	if d.Status != run.GateStatusRejected || d.Approved {
		t.Fatalf("expected a rejection: %+v", d)
	}
	if d.Reason != "预算不足" {
		t.Fatalf("reason = %q, want the reviewer's comment", d.Reason)
	}
}

// ── Timeout policies ─────────────────────────────────────────────────

func TestResolveTimedOutGates_AppliesEachPolicy(t *testing.T) {
	cases := []struct {
		policy      run.TimeoutPolicy
		wantStatus  run.GateStatus
		wantApprove bool
	}{
		{run.TimeoutAutoApprove, run.GateStatusApproved, true},
		{run.TimeoutAutoReject, run.GateStatusRejected, false},
		{run.TimeoutAbort, run.GateStatusTimeout, false},
	}
	for _, tc := range cases {
		h := newHarness()
		h.gates.timedOut = []run.Gate{{ID: 1, RunID: "run-1", Node: "review", OnTimeout: tc.policy}}

		if err := h.svc.ResolveTimedOutGates(context.Background()); err != nil {
			t.Fatalf("%s: %v", tc.policy, err)
		}
		d := h.gates.resolved[1]
		if d.Status != tc.wantStatus || d.Approved != tc.wantApprove {
			t.Fatalf("%s: got %+v, want status=%s approved=%v", tc.policy, d, tc.wantStatus, tc.wantApprove)
		}
		if _, ok := h.notifier.notified[1]; !ok {
			t.Fatalf("%s: the waiting run was not unblocked", tc.policy)
		}
		if len(h.audit.entries) != 1 || h.audit.entries[0].actor != nil {
			t.Fatalf("%s: a timeout has no human actor: %+v", tc.policy, h.audit.entries)
		}
	}
}

// An unrecognised or absent policy must not become approval: a gate exists
// because somebody wanted a decision, and silence is not one.
func TestParseTimeoutPolicy_DefaultsToAbort(t *testing.T) {
	for _, raw := range []string{"", "unknown", "APPROVE"} {
		if got := run.ParseTimeoutPolicy(raw); got != run.TimeoutAbort {
			t.Fatalf("ParseTimeoutPolicy(%q) = %v, want abort", raw, got)
		}
	}
}

func TestResolveTimedOutGates_SkipsGatesResolvedInTheMeantime(t *testing.T) {
	h := newHarness()
	h.gates.timedOut = []run.Gate{{ID: 1, RunID: "run-1", Node: "review", OnTimeout: run.TimeoutAbort}}
	h.gates.resolveErr = run.ErrGateResolved

	if err := h.svc.ResolveTimedOutGates(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(h.notifier.notified) != 0 || len(h.audit.entries) != 0 {
		t.Fatal("a gate a human already resolved must be left alone")
	}
}

// ── Limits ───────────────────────────────────────────────────────────

func TestLimits_BreachReportsTheFirstDimensionCrossed(t *testing.T) {
	limits := run.Limits{MaxTotalTokens: 100, MaxCostUSD: 1}

	if msg := limits.Breach(run.Usage{TotalTokens: 100, CostUSD: 1}); msg != "" {
		t.Fatalf("exactly at the limit is still within it, got %q", msg)
	}
	if msg := limits.Breach(run.Usage{TotalTokens: 101}); msg == "" {
		t.Fatal("expected a token breach")
	}
	if msg := limits.Breach(run.Usage{CostUSD: 1.01}); msg == "" {
		t.Fatal("expected a cost breach")
	}
}

func TestLimits_ZeroMeansUnbounded(t *testing.T) {
	if msg := (run.Limits{}).Breach(run.Usage{TotalTokens: 1 << 40, CostUSD: 1e6}); msg != "" {
		t.Fatalf("an unset limit must not fire: %q", msg)
	}
}

// ── Pagination ───────────────────────────────────────────────────────

func TestList_OverFetchesAndReportsNextOffset(t *testing.T) {
	h := newHarness()
	for i := 0; i < 25; i++ {
		h.repo.list = append(h.repo.list, run.Run{ID: "run-" + string(rune('a'+i))})
	}

	page, err := h.svc.List(context.Background(), 5, run.ListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 20 || !page.HasMore {
		t.Fatalf("expected a full first page with more to come: %d items, has_more=%v", len(page.Items), page.HasMore)
	}
	if page.NextCursor != "20" {
		t.Fatalf("next cursor = %q, want the next offset", page.NextCursor)
	}
	if h.repo.lastQ.Limit != 21 {
		t.Fatalf("the repository should be asked for limit+1 rows, got %d", h.repo.lastQ.Limit)
	}
	if h.repo.lastQ.TriggeredBy != 5 {
		t.Fatal("the list must always be scoped to the caller")
	}
}

func TestList_LastPageHasNoCursor(t *testing.T) {
	h := newHarness()
	h.repo.list = []run.Run{{ID: "run-1"}}

	page, err := h.svc.List(context.Background(), 5, run.ListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.HasMore || page.NextCursor != "" {
		t.Fatalf("expected the last page to carry no cursor: %+v", page)
	}
}

func TestList_EmptyPageIsAnEmptySliceNotNil(t *testing.T) {
	h := newHarness()
	page, err := h.svc.List(context.Background(), 5, run.ListQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Items == nil {
		t.Fatal("items must serialise as [] rather than null")
	}
}

// 试运行的对象是调用者自己刚写的 Agent，不适用 spec-11 那条"不能让订阅者
// 知道 Bundle 用了什么"的脱敏约束。一句含糊的"Provider 未配置"只会让人去
// 猜是哪一个、猜是不是没保存成功——最常见的真实原因是建提供商时没填 API
// Key。
func TestStartAgentTest_ProviderMissing_NamesTheProvider(t *testing.T) {
	h := newHarness()
	h.svc = h.svc.WithAgentTestRuns(&fakeTestBundles{})
	h.deps.status = run.DependencyProviderMissing
	h.deps.missing = []string{"my-deepseek"}

	_, err := h.svc.StartAgentTest(context.Background(), 1, run.AgentTestCommand{
		Definition: map[string]any{"agent": "a", "model": map[string]any{"provider": "my-deepseek"}},
	})

	de := mustDomainErr(t, err)
	if de.Code != domain.CodeProviderNotConfigured {
		t.Fatalf("错误码不对: %d", de.Code)
	}
	if !strings.Contains(de.Message, "my-deepseek") {
		t.Fatalf("报错里应点名具体的提供商: %s", de.Message)
	}
	if !strings.Contains(de.Message, "API Key") {
		t.Fatalf("报错里应说清要补什么: %s", de.Message)
	}
}

// 正式运行（可能是订阅者跑别人的 Bundle）仍然脱敏——那条约束没有被这次改
// 动放宽。
func TestStart_ProviderMissing_StaysGeneric(t *testing.T) {
	h := newHarness()
	h.deps.status = run.DependencyProviderMissing
	h.deps.missing = []string{"my-deepseek"}

	_, err := h.svc.Start(context.Background(), 1, run.StartCommand{BundleRef: "b"})

	de := mustDomainErr(t, err)
	if de.Code != domain.CodeProviderNotConfigured {
		t.Fatalf("错误码不对: %d", de.Code)
	}
	if strings.Contains(de.Message, "my-deepseek") {
		t.Fatalf("正式运行的报错不该泄漏 Bundle 用了哪个提供商: %s", de.Message)
	}
}

// ── 多轮对话 ─────────────────────────────────────────────────────────

// 没带 session_id 就开一段新会话，并把 id 回给调用方——前端拿它续下一条。
func TestStart_WithoutSessionIDOpensANewOne(t *testing.T) {
	h := newHarness()
	h.orch.started = make(chan struct{})
	h.resolver.resolved = run.ResolvedBundle{BundleID: 7, Ref: "content-pipeline", Version: "2.1"}

	created, err := h.svc.Start(context.Background(), 1, run.StartCommand{BundleRef: "content-pipeline"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-h.orch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("execution 没被启动")
	}

	if created.SessionID != "sess-generated" {
		t.Fatalf("响应里应当带上新开的会话 id，得到 %q", created.SessionID)
	}
	if h.orch.sessionID != "sess-generated" {
		t.Fatalf("会话 id 要传给 execution，得到 %q", h.orch.sessionID)
	}
}

// 带了 session_id 就接上那段对话——这条是"连着发消息模型记得上文"的地基。
// 以前这里传的是 runID，每次运行都是全新会话。
func TestStart_WithSessionIDContinuesThatConversation(t *testing.T) {
	h := newHarness()
	h.orch.started = make(chan struct{})
	h.resolver.resolved = run.ResolvedBundle{BundleID: 7, Ref: "content-pipeline", Version: "2.1"}

	created, err := h.svc.Start(context.Background(), 1, run.StartCommand{
		BundleRef: "content-pipeline", SessionID: "sess-已有的",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-h.orch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("execution 没被启动")
	}

	if created.SessionID != "sess-已有的" || h.orch.sessionID != "sess-已有的" {
		t.Fatalf("应当接上传进来的会话，run=%q execution=%q", created.SessionID, h.orch.sessionID)
	}
	if h.orch.sessionID == created.ID {
		t.Fatal("会话 id 不该退化成 runID")
	}
}

// 试运行面板走的是另一条入口，同样要能续上——用户在里面连着问几句是常态。
func TestStartAgentTest_CarriesTheSessionThrough(t *testing.T) {
	h := newHarness()
	h.orch.started = make(chan struct{})
	svc := h.svc.WithAgentTestRuns(&fakeTestBundles{})

	created, err := svc.StartAgentTest(context.Background(), 1, run.AgentTestCommand{
		Definition: map[string]any{"agent": "writer"},
		Input:      map[string]any{"message": "接着上一句说"},
		SessionID:  "sess-已有的",
	})
	if err != nil {
		t.Fatalf("start agent test: %v", err)
	}
	select {
	case <-h.orch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("execution 没被启动")
	}
	if created.SessionID != "sess-已有的" || h.orch.sessionID != "sess-已有的" {
		t.Fatalf("试运行也要接上会话，run=%q execution=%q", created.SessionID, h.orch.sessionID)
	}
}

// 一段对话由多次运行组成；刷新页面后前端靠这个列表把整段重建出来。别人的
// 对话读不到——查询按 (调用者, session_id) 走。
func TestListSession_ReturnsOnlyThisUsersRunsInThatConversation(t *testing.T) {
	h := newHarness()
	h.repo.list = []run.Run{
		{ID: "run-1", TriggeredBy: 1, SessionID: "sess-a"},
		{ID: "run-2", TriggeredBy: 1, SessionID: "sess-a"},
		{ID: "run-3", TriggeredBy: 1, SessionID: "sess-b"},
		{ID: "run-4", TriggeredBy: 2, SessionID: "sess-a"},
	}

	rows, err := h.svc.ListSession(context.Background(), 1, "sess-a")
	if err != nil {
		t.Fatalf("list session: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != "run-1" || rows[1].ID != "run-2" {
		t.Fatalf("只该拿到自己在 sess-a 里的两次运行，得到 %+v", rows)
	}

	if _, err := h.svc.ListSession(context.Background(), 1, "  "); err == nil {
		t.Fatal("空 session_id 应当报字段错误")
	}

	// 陌生 id 是空列表而不是 404：它可能是前端刚生成、还没跑过任何运行的
	// 新会话。
	rows, err = h.svc.ListSession(context.Background(), 1, "sess-还没跑过")
	if err != nil || len(rows) != 0 {
		t.Fatalf("陌生会话应当是空列表，得到 %+v / %v", rows, err)
	}
}
