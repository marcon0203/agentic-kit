package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// gateResolution is what unblocks a pending human_gates.Wait call, however
// it was decided — a human calling POST /runs/{id}/gate, or the timeout
// scanner applying on_timeout.
type gateResolution struct {
	approved bool
	reason   string // populated when !approved, becomes the branch's error
}

// gateRegistry is the in-process channel registry backing every currently
// pending gate, keyed by the human_gates row id. This is the same
// "ChannelGateProvider" design the POC used (架构设计文档: "POC 阶段的
// ChannelGateProvider（内存 channel）明确标注过服务重启会丢失所有待处理的
// gate这个局限") — durable, restart-survivable gates need a
// session.Service backed by this platform's own tables, deferred exactly
// as spec-10 flagged when it kept the GateWaiter interface abstract.
type gateRegistry struct {
	mu sync.Mutex
	ch map[int64]chan gateResolution
}

func newGateRegistry() *gateRegistry {
	return &gateRegistry{ch: map[int64]chan gateResolution{}}
}

// NewGateRegistry is newGateRegistry exported for cmd/server, which must
// construct one to share between NewRunEngine and RunGateTimeoutScanner.
func NewGateRegistry() *gateRegistry {
	return newGateRegistry()
}

func (r *gateRegistry) register(gateID int64) chan gateResolution {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan gateResolution, 1)
	r.ch[gateID] = ch
	return ch
}

func (r *gateRegistry) unregister(gateID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ch, gateID)
}

// resolve delivers res to gateID's waiter, if one is currently registered
// (it might not be — e.g. the process restarted, or it already resolved).
// Returns whether a waiter actually received it.
func (r *gateRegistry) resolve(gateID int64, res gateResolution) bool {
	r.mu.Lock()
	ch, ok := r.ch[gateID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- res:
		return true
	default:
		return false
	}
}

// gateNodeConfig is one Bundle.orchestration.human_gates[] entry, parsed
// once at run-compile time.
type gateNodeConfig struct {
	TimeoutSeconds *int32
	OnTimeout      string
	ApproverRoles  []string
}

// runGateWaiter implements adk.GateWaiter for one run: on Wait, it
// persists a human_gates row (so the approval endpoint and timeout
// scanner have something to act on) and blocks on the registry's channel
// for that row until a human resolves it, the timeout scanner does, or
// the run's own context is cancelled (spec-11's stop-in-5-seconds cancel).
type runGateWaiter struct {
	queries  store.Querier
	registry *gateRegistry
	runID    string
	configs  map[string]gateNodeConfig
}

func newRunGateWaiter(q store.Querier, registry *gateRegistry, runID string, configs map[string]gateNodeConfig) adk.GateWaiter {
	return &runGateWaiter{queries: q, registry: registry, runID: runID, configs: configs}
}

func (w *runGateWaiter) Wait(ctx context.Context, node string) error {
	cfg := w.configs[node]

	var timeoutSeconds pgtype.Int4
	if cfg.TimeoutSeconds != nil {
		timeoutSeconds = pgtype.Int4{Valid: true, Int32: *cfg.TimeoutSeconds}
	}
	onTimeout := cfg.OnTimeout
	if onTimeout == "" {
		onTimeout = "abort"
	}
	roles := cfg.ApproverRoles
	if roles == nil {
		roles = []string{}
	}
	approverRoles, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("adk: encode approver_roles for node %q: %w", node, err)
	}

	row, err := w.queries.CreateHumanGate(ctx, store.CreateHumanGateParams{
		RunID: w.runID, Node: node, TimeoutSeconds: timeoutSeconds, OnTimeout: onTimeout, ApproverRoles: approverRoles,
	})
	if err != nil {
		return fmt.Errorf("adk: persist human gate for node %q: %w", node, err)
	}

	ch := w.registry.register(row.ID)
	defer w.registry.unregister(row.ID)

	select {
	case res := <-ch:
		if !res.approved {
			return fmt.Errorf("human gate after %q: %s", node, res.reason)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
