// Package orchestrator adapts Google's ADK to the run bounded context: it
// compiles a Bundle definition into a runnable agent graph, drives it, and
// translates what comes back into domain events.
//
// Everything here is infrastructure. The rules about what a run may show,
// when it may start and who may approve a gate live in
// internal/domain/run; this package only knows how to execute.
package orchestrator

import (
	"context"
	"sync"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// GateRegistry is the in-process channel registry backing every currently
// pending gate, keyed by the human_gates row id.
//
// This is the POC's ChannelGateProvider design, kept deliberately (架构设计
// 文档: "POC 阶段的 ChannelGateProvider（内存 channel）明确标注过服务重启会
// 丢失所有待处理的 gate 这个局限"). Durable, restart-survivable gates need a
// session service backed by this platform's own tables — deferred exactly
// as spec-10 flagged when it kept the waiter abstract. The domain talks to
// it through run.GateNotifier and does not know it is a channel.
type GateRegistry struct {
	mu sync.Mutex
	ch map[int64]chan run.Decision
}

func NewGateRegistry() *GateRegistry { return &GateRegistry{ch: map[int64]chan run.Decision{}} }

var _ run.GateNotifier = (*GateRegistry)(nil)

func (r *GateRegistry) register(gateID int64) chan run.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan run.Decision, 1)
	r.ch[gateID] = ch
	return ch
}

func (r *GateRegistry) unregister(gateID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ch, gateID)
}

// Notify delivers a decision to gateID's waiter if one is registered here.
// It might not be — the process may have restarted, taking the waiting run
// with it — which is reported rather than treated as an error.
func (r *GateRegistry) Notify(gateID int64, d run.Decision) bool {
	r.mu.Lock()
	ch, ok := r.ch[gateID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- d:
		return true
	default:
		return false
	}
}

// gateWaiter implements adk.GateWaiter for one run: on Wait it persists a
// pending gate (so the approval endpoint and the timeout scanner have
// something to act on), emits the event spec-14's gate card renders, then
// blocks until a decision arrives or the run's context is cancelled.
type gateWaiter struct {
	gates    run.GateRepository
	events   run.EventStore
	registry *GateRegistry
	runID    string
	configs  map[string]run.GateConfig
}

func (w *gateWaiter) Wait(ctx context.Context, node string) error {
	cfg, ok := w.configs[node]
	if !ok {
		cfg = run.GateConfig{OnTimeout: run.TimeoutAbort}
	}
	cfg.Node = node

	gate, err := w.gates.CreatePending(ctx, w.runID, cfg)
	if err != nil {
		return err
	}

	_ = w.events.Append(ctx, run.Event{
		RunID: w.runID, Type: run.EventGateWaiting, Node: node,
		Payload: map[string]any{"gate_id": gate.ID, "on_timeout": string(cfg.OnTimeout)},
	})

	ch := w.registry.register(gate.ID)
	defer w.registry.unregister(gate.ID)

	select {
	case d := <-ch:
		if !d.Approved {
			return &gateRejectedError{node: node, reason: d.Reason}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// gateRejectedError is the branch-stopping error a non-approval produces.
type gateRejectedError struct {
	node   string
	reason string
}

func (e *gateRejectedError) Error() string {
	return "human gate after " + e.node + ": " + e.reason
}

var _ adk.GateWaiter = (*gateWaiter)(nil)
