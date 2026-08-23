package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcon0203/agentic-kit/internal/adapter/postgres"
	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// Engine implements run.Orchestrator. It is the only place that wires the
// ADK compiler to the model gateway and to persistence.
type Engine struct {
	queries  store.Querier
	runs     run.Repository
	events   run.EventStore
	gates    run.GateRepository
	registry *GateRegistry
	keys     providerKeys
	aesKey   []byte
	appName  string

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

// providerKeys is the owner's decrypted model credentials, which the
// compiler needs to build a working gateway.
type providerKeys interface {
	Keys(ctx context.Context, ownerID int64) (map[string]string, error)
}

func NewEngine(
	queries store.Querier, runs run.Repository, events run.EventStore,
	gates run.GateRepository, registry *GateRegistry, keys providerKeys, aesKey []byte,
) *Engine {
	return &Engine{
		queries: queries, runs: runs, events: events, gates: gates, registry: registry,
		keys: keys, aesKey: aesKey, appName: "agentic-kit", cancels: map[string]context.CancelFunc{},
	}
}

var _ run.Orchestrator = (*Engine)(nil)

// Prepare compiles one Bundle for one run: every agents[] entry through
// adk.CompileAgent (each resource reference gated by the authorizer), then
// the whole graph through adk.CompileBundle.
func (e *Engine) Prepare(ctx context.Context, runID string, b run.ResolvedBundle, gateConfigs map[string]run.GateConfig) (run.Execution, error) {
	graph, err := bundlegraph.ParseGraph(b.Definition)
	if err != nil {
		return nil, fmt.Errorf("parse orchestration graph: %w", err)
	}

	apiKeys, err := e.keys.Keys(ctx, b.OwnerUserID)
	if err != nil {
		return nil, fmt.Errorf("load provider keys: %w", err)
	}
	gateway := modelgateway.NewGateway(nil)
	authorizer := newResourceAuthorizer(ctx, e.queries, b.OwnerUserID, e.aesKey)

	agentsRaw, _ := b.Definition["agents"].([]any)
	compiled := make(map[string]adk.CompiledAgent, len(agentsRaw))
	for _, a := range agentsRaw {
		am, _ := a.(map[string]any)
		ref, _ := am["ref"].(string)
		alias, _ := am["alias"].(string)
		version, _ := am["version"].(string)

		// An agent is compiled under its Bundle-local node name, which is
		// its alias when it has one — the same name the graph's edges use.
		node := ref
		if alias != "" {
			node = alias
		}

		agentDef, err := e.loadAgentDefinition(ctx, b.OwnerUserID, ref, version)
		if err != nil {
			return nil, fmt.Errorf("resolve agent %q: %w", ref, err)
		}
		agentDef["agent"] = node

		compiledAgent, err := adk.CompileAgent(ctx, agentDef, adk.AgentCompileOptions{Gateway: gateway, APIKeys: apiKeys, Authorizer: authorizer})
		if err != nil {
			return nil, fmt.Errorf("compile agent %q: %w", node, err)
		}
		compiled[node] = compiledAgent
	}

	gateNodes := make(map[string]bool, len(gateConfigs))
	for node := range gateConfigs {
		gateNodes[node] = true
	}
	waiter := &gateWaiter{gates: e.gates, events: e.events, registry: e.registry, runID: runID, configs: gateConfigs}

	bundleRef, _ := b.Definition["bundle"].(string)
	root, err := adk.CompileBundle(adk.BundleCompileOptions{
		BundleRef: bundleRef, Graph: graph, Agents: compiled, GateNodes: gateNodes, GateWaiter: waiter,
	})
	if err != nil {
		return nil, err
	}
	return &execution{engine: e, runID: runID, root: root}, nil
}

func (e *Engine) loadAgentDefinition(ctx context.Context, ownerID int64, ref, version string) (map[string]any, error) {
	// The same resolution the pre-flight dependency check just performed —
	// shared rather than reimplemented, so "which version was checked" and
	// "which version runs" cannot drift apart.
	row, err := postgres.ResolveAgentVersion(ctx, e.queries, ownerID, ref, version)
	if err != nil {
		return nil, err
	}
	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return nil, err
	}
	return def, nil
}

// Cancel implements POST /runs/{id}/cancel's "5 秒内生效": it cancels the
// run's context in-process. Output, events and usage already produced are
// kept (spec-11) — only forward progress stops.
func (e *Engine) Cancel(runID string) bool {
	e.cancelMu.Lock()
	cancel, ok := e.cancels[runID]
	e.cancelMu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func (e *Engine) registerCancel(runID string, cancel context.CancelFunc) {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	e.cancels[runID] = cancel
}

func (e *Engine) unregisterCancel(runID string) {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	delete(e.cancels, runID)
}

// execution is one compiled Bundle waiting to be driven.
type execution struct {
	engine *Engine
	runID  string
	root   adk.CompiledAgent
}

// Start drives the run to completion, persisting each translated event and
// accumulating usage as it goes, enforcing the circuit breaker, and writing
// the terminal status.
//
// Every persistence call uses a fresh background context rather than the
// run's own: cancelling a run must still record why it stopped, and a
// cancelled context would silently drop exactly the events that explain it.
func (x *execution) Start(triggeredBy int64, input map[string]any, limits run.Limits) {
	e := x.engine
	ctx := context.Background()
	if limits.MaxWallClockSeconds > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, time.Duration(limits.MaxWallClockSeconds)*time.Second)
		defer cancelTimeout()
	}
	ctx, cancel := context.WithCancel(ctx)
	e.registerCancel(x.runID, cancel)
	defer e.unregisterCancel(x.runID)
	defer cancel()

	persist := context.Background()

	runner, err := adk.NewADKRunner(e.appName, x.root)
	if err != nil {
		x.finish(persist, run.StatusFailed, err.Error())
		return
	}

	msg, err := json.Marshal(input)
	if err != nil {
		msg = []byte("{}")
	}

	_ = e.events.Append(persist, run.Event{RunID: x.runID, Type: run.EventBundleStarted})

	var usage run.Usage
	breached := ""

	_, runErr := runner.Run(ctx, adk.AgentInput{UserID: fmt.Sprint(triggeredBy), SessionID: x.runID, Message: string(msg)},
		func(ev adk.Event) {
			payload := map[string]any{}
			if b, err := json.Marshal(ev.Payload); err == nil {
				_ = json.Unmarshal(b, &payload)
			}
			_ = e.events.Append(persist, run.Event{
				RunID: x.runID, Type: ev.Type, Node: ev.Node, Payload: payload, IsInternal: ev.IsInternal,
			})

			if ev.InputTokens == 0 && ev.OutputTokens == 0 && ev.CostUSD == 0 {
				return
			}
			usage.TotalTokens += ev.InputTokens + ev.OutputTokens
			usage.CostUSD += ev.CostUSD
			_ = e.runs.AddUsage(persist, x.runID, usage.TotalTokens, usage.CostUSD)

			if breached = limits.Breach(usage); breached != "" {
				cancel()
			}
		})

	switch {
	case breached != "":
		x.finish(persist, run.StatusFailed, breached)
	case isDeadlineExceeded(runErr):
		x.finish(persist, run.StatusFailed, run.FailWallClockExceeded)
	case isCanceled(runErr):
		x.finish(persist, run.StatusFailed, run.FailCancelledByUser)
	case runErr != nil:
		x.finish(persist, run.StatusFailed, sanitizeRunError(runErr))
	default:
		x.finish(persist, run.StatusFinished, "")
	}
}

func (x *execution) finish(ctx context.Context, status run.Status, errMsg string) {
	_ = x.engine.runs.UpdateStatus(ctx, x.runID, status, errMsg)

	eventType := run.EventBundleFinished
	var payload map[string]any
	if errMsg != "" {
		eventType, payload = run.EventBundleFailed, map[string]any{"error": errMsg}
	}
	_ = x.engine.events.Append(ctx, run.Event{RunID: x.runID, Type: eventType, Payload: payload})
}

// sanitizeRunError strips anything that might name a private resource from
// an execution failure before it is stored as bundle_runs.error. spec-11's
// "错误信息必须脱敏" applies mid-run just as much as at the pre-flight check
// — a subscriber watching a failure must not learn what the Bundle is
// built from. Only the provider-exhaustion case is specific enough to be
// worth reporting, and it names no resource.
func sanitizeRunError(err error) string {
	if strings.Contains(err.Error(), "modelgateway: all providers") {
		return run.FailAllProvidersDown
	}
	return run.FailGeneric
}
