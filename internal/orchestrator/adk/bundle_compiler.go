package adk

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
)

// defaultMaxLoopIterations bounds a self-loop retry edge (spec-10's
// tests_passed==false pattern) so a condition that never flips true can't
// spin the graph forever — a safety net, not a product-configurable limit.
const defaultMaxLoopIterations = 5

// GateWaiter pauses graph execution after a human_gates[].after node until
// a human resolves it (spec-10 §"human_gates[].after → human-in-the-loop
// 确认节点"). Its durable, restart-survivable backing (per spec-11's
// timeout-scanning job) is built in task 11 — this package only needs the
// blocking primitive, so run-engine tests can inject a real waiter and
// compiler tests can inject a mock or none at all (nil skips gates).
type GateWaiter interface {
	// Wait blocks until node's gate is resolved, or ctx is done. A non-nil
	// error (e.g. "rejected", "aborted on timeout") stops the branch.
	Wait(ctx context.Context, node string) error
}

// BundleCompileOptions carries everything CompileBundle needs to turn one
// validated orchestration graph (internal/bundlegraph, already run through
// Validate at Bundle-save time) into an executable ADK agent.
type BundleCompileOptions struct {
	BundleRef string
	Graph     bundlegraph.Graph
	// Agents maps each graph node name to its already-compiled ADK agent
	// (see CompileAgent) — one per Bundle.agents[] ref/alias.
	Agents map[string]agent.Agent
	// GateNodes are the node names with a human_gates[].after entry.
	GateNodes  map[string]bool
	GateWaiter GateWaiter // nil: gates are structurally present but skipped
	// MaxLoopIterations overrides defaultMaxLoopIterations; 0 uses the default.
	MaxLoopIterations int
}

// CompileBundle compiles opts into a single root ADK agent whose Run
// function walks the Bundle's orchestration graph — sequential edges run
// in order, edges sharing a source run concurrently as soon as they're
// each individually ready, a `join` edge only fires its target once every
// distinct predecessor has completed (spec-07/spec-10's wait_all), and a
// self-loop edge re-invokes the same node rather than being a distinct
// target (so it can never itself deadlock a join — spec-10's explicit
// regression requirement).
//
// The graph *walk* is product-specific logic this package owns (ADK has no
// notion of our DSL's arbitrary conditional graph); everything below a
// scheduling decision — the LLM call loop, tool calling, session state
// storage — is ADK's, via each compiled sub-agent's own Run.
func CompileBundle(opts BundleCompileOptions) (agent.Agent, error) {
	if opts.Graph.Entry == "" {
		return nil, fmt.Errorf("adk: bundle %q: graph has no entry", opts.BundleRef)
	}
	for _, node := range opts.Graph.Nodes {
		if _, ok := opts.Agents[node]; !ok {
			return nil, fmt.Errorf("adk: bundle %q: no compiled agent for node %q", opts.BundleRef, node)
		}
	}

	programs := make(map[int]*vm.Program, len(opts.Graph.Edges))
	for _, e := range opts.Graph.Edges {
		if e.Condition == "" {
			continue
		}
		p, err := expr.Compile(e.Condition, expr.Env(conditionEnv{}), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("adk: bundle %q: edge[%d].condition: %w", opts.BundleRef, e.Index, err)
		}
		programs[e.Index] = p
	}

	adjacency := map[string][]bundlegraph.Edge{}
	predecessors := map[string]map[string]bool{}
	joinAny := map[string]bool{}
	for _, e := range opts.Graph.Edges {
		adjacency[e.From] = append(adjacency[e.From], e)
		for _, to := range e.To {
			if to == bundlegraph.EndNode || to == e.From {
				continue // END isn't a schedulable node; a self-loop has no "predecessor" bookkeeping
			}
			if predecessors[to] == nil {
				predecessors[to] = map[string]bool{}
			}
			predecessors[to][e.From] = true
			if e.Join == "wait_any" {
				joinAny[to] = true
			}
		}
	}
	incomingTotal := map[string]int{}
	for node, preds := range predecessors {
		incomingTotal[node] = len(preds)
	}

	maxLoop := opts.MaxLoopIterations
	if maxLoop <= 0 {
		maxLoop = defaultMaxLoopIterations
	}

	cfg := agent.Config{
		Name: opts.BundleRef,
		Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return runGraph(ic, graphPlan{
				entry:         opts.Graph.Entry,
				agents:        opts.Agents,
				adjacency:     adjacency,
				programs:      programs,
				incomingTotal: incomingTotal,
				joinAny:       joinAny,
				gateNodes:     opts.GateNodes,
				gateWaiter:    opts.GateWaiter,
				maxLoop:       maxLoop,
			})
		},
	}
	root, err := agent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("adk: bundle %q: compile root agent: %w", opts.BundleRef, err)
	}
	return root, nil
}

// conditionEnv mirrors internal/bundlegraph's own (unexported) shape —
// edge.condition expressions only ever read shared_state, so re-declaring
// the same `shared_state`-tagged struct here keeps this package's runtime
// evaluation environment identical to what save-time syntax checking
// already validated against.
type conditionEnv struct {
	SharedState map[string]any `expr:"shared_state"`
}

type graphPlan struct {
	entry         string
	agents        map[string]agent.Agent
	adjacency     map[string][]bundlegraph.Edge
	programs      map[int]*vm.Program
	incomingTotal map[string]int
	joinAny       map[string]bool
	gateNodes     map[string]bool
	gateWaiter    GateWaiter
	maxLoop       int
}

type eventOrErr struct {
	ev  *session.Event
	err error
}

// executor holds one graph run's mutable scheduling state, shared by every
// node's goroutine.
//
// sharedState is this executor's own private view of shared_state, used
// only for routing (self-loop / conditional-edge) decisions — it is NOT
// read from ic.Session().State(). The ADK Runner that ultimately consumes
// this agent's event stream (see runner.go) also calls
// SessionService.AppendEvent for every event, which applies the same
// StateDelta into the *session's* state — but it does so asynchronously,
// only as fast as it drains ex.out, while this executor's own goroutines
// keep running ahead. Routing off the session's state was found to race:
// the Runner replaying an *older*, already-superseded event's StateDelta
// (e.g. a stale "tests_passed: false" from two loop iterations ago) could
// land after this executor had already moved the value to true, flipping
// it back and re-triggering an already-satisfied self-loop condition. A
// private, synchronously-updated map has no such lag.
type executor struct {
	plan graphPlan
	ic   agent.InvocationContext

	mu              sync.Mutex
	pendingIncoming map[string]int
	started         map[string]bool
	sharedState     map[string]any

	active sync.WaitGroup
	out    chan eventOrErr
}

func runGraph(ic agent.InvocationContext, plan graphPlan) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		ex := &executor{
			plan:            plan,
			ic:              ic,
			pendingIncoming: cloneIntMap(plan.incomingTotal),
			started:         map[string]bool{plan.entry: true},
			sharedState:     initialSharedState(ic),
			out:             make(chan eventOrErr, 32),
		}

		ex.active.Add(1)
		go ex.runNode(plan.entry)
		go func() {
			ex.active.Wait()
			close(ex.out)
		}()

		for item := range ex.out {
			if !yield(item.ev, item.err) {
				return
			}
		}
	}
}

// initialSharedState seeds the executor's private routing view from
// whatever shared_state the session already carries (e.g. a resumed run,
// or state a caller set via runner.WithStateDelta before this invocation).
func initialSharedState(ic agent.InvocationContext) map[string]any {
	state := map[string]any{}
	for k, v := range ic.Session().State().All() {
		state[k] = v
	}
	return state
}

func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (ex *executor) send(ev *session.Event, err error) {
	ex.out <- eventOrErr{ev: ev, err: err}
}

// applyStateDelta updates this executor's private sharedState view — the
// only copy its own routing decisions ever read. It never touches
// ic.Session().State() directly; that copy is updated independently (and
// correctly, in event order) by the ADK Runner via AppendEvent as it
// drains this agent's event stream, which is what actually persists it.
func (ex *executor) applyStateDelta(ev *session.Event) {
	if ev == nil || len(ev.Actions.StateDelta) == 0 {
		return
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	for k, v := range ev.Actions.StateDelta {
		ex.sharedState[k] = v
	}
}

func (ex *executor) readState() map[string]any {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	state := make(map[string]any, len(ex.sharedState))
	for k, v := range ex.sharedState {
		state[k] = v
	}
	return state
}

func (ex *executor) evalCondition(edgeIndex int) (bool, error) {
	program, ok := ex.plan.programs[edgeIndex]
	if !ok {
		return true, nil // no condition: unconditionally active
	}
	result, err := expr.Run(program, conditionEnv{SharedState: ex.readState()})
	if err != nil {
		return false, err
	}
	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("condition did not evaluate to a boolean")
	}
	return b, nil
}

// runNode drives one graph node to completion (including its own self-loop
// retries), then schedules whatever becomes ready next. It always calls
// ex.active.Done() exactly once, matching the Add(1) its caller made.
func (ex *executor) runNode(node string) {
	defer ex.active.Done()

	loopCount := 0
	for {
		a := ex.plan.agents[node]
		for ev, err := range a.Run(ex.ic) {
			ex.send(ev, err)
			if err != nil {
				return
			}
			ex.applyStateDelta(ev)
		}

		if ex.plan.gateNodes[node] && ex.plan.gateWaiter != nil {
			if err := ex.plan.gateWaiter.Wait(ex.ic, node); err != nil {
				ex.send(nil, fmt.Errorf("adk: human gate after %q: %w", node, err))
				return
			}
		}

		selfLoop := false
		var nextTargets []string
		seen := map[string]bool{}
		for _, edge := range ex.plan.adjacency[node] {
			active, err := ex.evalCondition(edge.Index)
			if err != nil {
				ex.send(nil, fmt.Errorf("adk: node %q: evaluate edge[%d].condition: %w", node, edge.Index, err))
				return
			}
			if !active {
				continue
			}
			for _, to := range edge.To {
				switch {
				case to == bundlegraph.EndNode:
					// This branch of the graph terminates here.
				case to == node:
					selfLoop = true
				case !seen[to]:
					seen[to] = true
					nextTargets = append(nextTargets, to)
				}
			}
		}

		if selfLoop && len(nextTargets) == 0 {
			loopCount++
			if loopCount >= ex.plan.maxLoop {
				ex.send(nil, fmt.Errorf("adk: node %q exceeded max loop iterations (%d)", node, ex.plan.maxLoop))
				return
			}
			continue
		}

		for _, target := range nextTargets {
			ex.scheduleIfReady(target)
		}
		return
	}
}

func (ex *executor) scheduleIfReady(target string) {
	ex.mu.Lock()
	ex.pendingIncoming[target]--
	ready := ex.plan.joinAny[target] || ex.pendingIncoming[target] <= 0
	alreadyStarted := ex.started[target]
	if ready && !alreadyStarted {
		ex.started[target] = true
	}
	ex.mu.Unlock()

	if ready && !alreadyStarted {
		ex.active.Add(1)
		go ex.runNode(target)
	}
}
