package adk

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
)

// mockLLMAgent builds an ADK agent whose Run function is entirely
// controlled by the test — no real model call — per spec-10 §4's explicit
// design intent ("单测可以用 Mock 实现跑通编排逻辑，不必真的调模型").
func mockLLMAgent(t *testing.T, name string, run func(ic agent.InvocationContext) *session.Event) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: name,
		Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := run(ic)
				ev.Author = name
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("build mock agent %q: %v", name, err)
	}
	return a
}

func finalTextEvent(ic agent.InvocationContext, text string, stateDelta map[string]any) *session.Event {
	ev := session.NewEventWithContext(ic, ic.InvocationID())
	ev.Content = genai.NewContentFromText(text, genai.RoleModel)
	if stateDelta != nil {
		ev.Actions.StateDelta = stateDelta
	}
	return ev
}

// webAppBuilderGraph mirrors schemas/examples/web-app-builder.bundle.yaml
// exactly: product_manager -> architect -> [ui_designer, fullstack_engineer]
// (parallel) -> ui_designer -> fullstack_engineer (wait_all join) ->
// fullstack_engineer -> END | fullstack_engineer (conditional self-loop).
func webAppBuilderGraph() bundlegraph.Graph {
	return bundlegraph.Graph{
		Nodes: []string{"product_manager", "architect", "ui_designer", "fullstack_engineer"},
		Entry: "product_manager",
		Edges: []bundlegraph.Edge{
			{Index: 0, From: "product_manager", To: []string{"architect"}, Mode: "sequential", Join: "wait_all"},
			{Index: 1, From: "architect", To: []string{"ui_designer", "fullstack_engineer"}, Mode: "parallel", Join: "wait_all"},
			{Index: 2, From: "ui_designer", To: []string{"fullstack_engineer"}, Mode: "sequential", Join: "wait_all"},
			{Index: 3, From: "fullstack_engineer", To: []string{bundlegraph.EndNode}, Condition: "shared_state.tests_passed == true", Mode: "sequential", Join: "wait_all"},
			{Index: 4, From: "fullstack_engineer", To: []string{"fullstack_engineer"}, Condition: "shared_state.tests_passed == false", Mode: "sequential", Join: "wait_all"},
		},
	}
}

func runCompiled(t *testing.T, root agent.Agent) []*session.Event {
	t.Helper()
	rn, err := runner.New(runner.Config{AppName: "test", Agent: root, SessionService: session.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []*session.Event
	for ev, err := range rn.Run(ctx, "user1", "sess1", genai.NewContentFromText("go", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestCompileBundle_WebAppBuilder_RunsToEnd(t *testing.T) {
	var fullstackRuns int
	var mu sync.Mutex

	agents := map[string]agent.Agent{
		"product_manager": mockLLMAgent(t, "product_manager", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "requirements gathered", nil)
		}),
		"architect": mockLLMAgent(t, "architect", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "architecture designed", nil)
		}),
		"ui_designer": mockLLMAgent(t, "ui_designer", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ui designed", nil)
		}),
		"fullstack_engineer": mockLLMAgent(t, "fullstack_engineer", func(ic agent.InvocationContext) *session.Event {
			mu.Lock()
			fullstackRuns++
			passed := fullstackRuns >= 2 // fails once, then passes — exercises the retry loop
			mu.Unlock()
			return finalTextEvent(ic, "code written", map[string]any{"tests_passed": passed})
		}),
	}

	root, err := CompileBundle(BundleCompileOptions{BundleRef: "web-app-builder", Graph: webAppBuilderGraph(), Agents: agents})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}

	events := runCompiled(t, root)

	seen := map[string]bool{}
	for _, ev := range events {
		seen[ev.Author] = true
	}
	for _, want := range []string{"product_manager", "architect", "ui_designer", "fullstack_engineer"} {
		if !seen[want] {
			t.Errorf("expected an event from %q, got authors: %v", want, seen)
		}
	}

	mu.Lock()
	runs := fullstackRuns
	mu.Unlock()
	if runs < 2 {
		t.Errorf("expected fullstack_engineer to retry at least once before passing, ran %d times", runs)
	}
}

// TestCompileBundle_ParallelBranchesRunConcurrently uses a simpler graph
// than the web-app-builder fixture on purpose: entry -> [branchA, branchB]
// (parallel), each independently -> END, with *no* join relationship
// between them. web-app-builder's own parallel branches (ui_designer,
// fullstack_engineer) can't demonstrate pure concurrency in isolation —
// fullstack_engineer also has a wait_all join on ui_designer (tested
// separately below), so it never starts running until ui_designer
// finishes, by design.
func TestCompileBundle_ParallelBranchesRunConcurrently(t *testing.T) {
	var mu sync.Mutex
	var aStart, aEnd, bStart, bEnd time.Time

	graph := bundlegraph.Graph{
		Nodes: []string{"entry", "branchA", "branchB"},
		Entry: "entry",
		Edges: []bundlegraph.Edge{
			{Index: 0, From: "entry", To: []string{"branchA", "branchB"}, Mode: "parallel", Join: "wait_all"},
			{Index: 1, From: "branchA", To: []string{bundlegraph.EndNode}},
			{Index: 2, From: "branchB", To: []string{bundlegraph.EndNode}},
		},
	}
	agents := map[string]agent.Agent{
		"entry": mockLLMAgent(t, "entry", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"branchA": mockLLMAgent(t, "branchA", func(ic agent.InvocationContext) *session.Event {
			mu.Lock()
			aStart = time.Now()
			mu.Unlock()
			time.Sleep(60 * time.Millisecond)
			mu.Lock()
			aEnd = time.Now()
			mu.Unlock()
			return finalTextEvent(ic, "ok", nil)
		}),
		"branchB": mockLLMAgent(t, "branchB", func(ic agent.InvocationContext) *session.Event {
			mu.Lock()
			bStart = time.Now()
			mu.Unlock()
			time.Sleep(60 * time.Millisecond)
			mu.Lock()
			bEnd = time.Now()
			mu.Unlock()
			return finalTextEvent(ic, "ok", nil)
		}),
	}

	root, err := CompileBundle(BundleCompileOptions{BundleRef: "parallel-test", Graph: graph, Agents: agents})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	runCompiled(t, root)

	if !aStart.Before(bEnd) || !bStart.Before(aEnd) {
		t.Fatalf("expected overlapping execution windows: a=[%v,%v] b=[%v,%v]", aStart, aEnd, bStart, bEnd)
	}
}

func TestCompileBundle_WaitAllJoin_WaitsForBothUpstream(t *testing.T) {
	var mu sync.Mutex
	var uiFinish, fsFirstStart time.Time

	agents := map[string]agent.Agent{
		"product_manager": mockLLMAgent(t, "product_manager", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"architect": mockLLMAgent(t, "architect", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"ui_designer": mockLLMAgent(t, "ui_designer", func(ic agent.InvocationContext) *session.Event {
			time.Sleep(80 * time.Millisecond) // deliberately the slow branch
			mu.Lock()
			uiFinish = time.Now()
			mu.Unlock()
			return finalTextEvent(ic, "ok", nil)
		}),
		"fullstack_engineer": mockLLMAgent(t, "fullstack_engineer", func(ic agent.InvocationContext) *session.Event {
			mu.Lock()
			if fsFirstStart.IsZero() {
				fsFirstStart = time.Now()
			}
			mu.Unlock()
			return finalTextEvent(ic, "ok", map[string]any{"tests_passed": true})
		}),
	}

	root, err := CompileBundle(BundleCompileOptions{BundleRef: "web-app-builder", Graph: webAppBuilderGraph(), Agents: agents})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	runCompiled(t, root)

	// fullstack_engineer has two predecessors: architect (direct parallel
	// branch) and ui_designer (the wait_all join edge). Its join-satisfying
	// run must not start before ui_designer — the slower predecessor —
	// actually finishes.
	if fsFirstStart.Before(uiFinish) {
		t.Fatalf("fullstack_engineer's joined run started (%v) before ui_designer finished (%v)", fsFirstStart, uiFinish)
	}
}

func TestCompileBundle_SelfLoopRetry_EventuallyReachesEnd(t *testing.T) {
	var runs int
	var mu sync.Mutex

	agents := map[string]agent.Agent{
		"product_manager": mockLLMAgent(t, "product_manager", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"architect": mockLLMAgent(t, "architect", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"ui_designer": mockLLMAgent(t, "ui_designer", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"fullstack_engineer": mockLLMAgent(t, "fullstack_engineer", func(ic agent.InvocationContext) *session.Event {
			mu.Lock()
			runs++
			n := runs
			mu.Unlock()
			return finalTextEvent(ic, "ok", map[string]any{"tests_passed": n >= 3})
		}),
	}

	root, err := CompileBundle(BundleCompileOptions{BundleRef: "web-app-builder", Graph: webAppBuilderGraph(), Agents: agents, MaxLoopIterations: 10})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}

	// The self-loop must terminate this test on its own (no deadlock) —
	// the 10s timeout in runCompiled is the regression guard for spec-10's
	// explicit "自环边不导致 join 死锁" requirement.
	runCompiled(t, root)

	mu.Lock()
	defer mu.Unlock()
	if runs != 3 {
		t.Fatalf("expected exactly 3 runs (2 retries + 1 pass), got %d", runs)
	}
}

func TestCompileBundle_SelfLoopExceedsMaxIterations_ReturnsError(t *testing.T) {
	agents := map[string]agent.Agent{
		"product_manager": mockLLMAgent(t, "product_manager", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"architect": mockLLMAgent(t, "architect", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"ui_designer": mockLLMAgent(t, "ui_designer", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"fullstack_engineer": mockLLMAgent(t, "fullstack_engineer", func(ic agent.InvocationContext) *session.Event {
			// Never passes — must be stopped by the safety cap, not hang.
			return finalTextEvent(ic, "ok", map[string]any{"tests_passed": false})
		}),
	}

	root, err := CompileBundle(BundleCompileOptions{BundleRef: "web-app-builder", Graph: webAppBuilderGraph(), Agents: agents, MaxLoopIterations: 3})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}

	rn, err := runner.New(runner.Config{AppName: "test", Agent: root, SessionService: session.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var gotErr error
	for _, err := range rn.Run(ctx, "user1", "sess1", genai.NewContentFromText("go", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "exceeded max loop iterations") {
		t.Fatalf("expected a max-loop-iterations error, got %v", gotErr)
	}
}

type mockGateWaiter struct {
	waited []string
	err    error
}

func (m *mockGateWaiter) Wait(_ context.Context, node string) error {
	m.waited = append(m.waited, node)
	return m.err
}

func TestCompileBundle_HumanGate_BlocksUntilResolved(t *testing.T) {
	agents := map[string]agent.Agent{
		"product_manager": mockLLMAgent(t, "product_manager", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"architect": mockLLMAgent(t, "architect", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"ui_designer": mockLLMAgent(t, "ui_designer", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", nil)
		}),
		"fullstack_engineer": mockLLMAgent(t, "fullstack_engineer", func(ic agent.InvocationContext) *session.Event {
			return finalTextEvent(ic, "ok", map[string]any{"tests_passed": true})
		}),
	}
	waiter := &mockGateWaiter{}

	root, err := CompileBundle(BundleCompileOptions{
		BundleRef: "web-app-builder", Graph: webAppBuilderGraph(), Agents: agents,
		GateNodes: map[string]bool{"architect": true, "fullstack_engineer": true}, GateWaiter: waiter,
	})
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	runCompiled(t, root)

	if len(waiter.waited) < 2 {
		t.Fatalf("expected both gates to be waited on, got %v", waiter.waited)
	}
}

func TestCompileBundle_UnknownNode_ReturnsCompileError(t *testing.T) {
	graph := bundlegraph.Graph{Nodes: []string{"a"}, Entry: "a", Edges: nil}
	_, err := CompileBundle(BundleCompileOptions{BundleRef: "b", Graph: graph, Agents: map[string]agent.Agent{}})
	if err == nil {
		t.Fatal("expected an error when a graph node has no compiled agent")
	}
}
