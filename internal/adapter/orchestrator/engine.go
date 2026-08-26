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
	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// Engine implements run.Orchestrator. It is the only place that wires the
// ADK compiler to the model gateway and to persistence.
type Engine struct {
	queries    store.Querier
	runs       run.Repository
	events     run.EventStore
	gates      run.GateRepository
	registry   *GateRegistry
	keys       providerKeys
	aesKey     []byte
	appName    string
	kbSearcher adk.KnowledgeBaseSearcher
	skills     adk.SkillContentFetcher
	plugins    adk.PluginRuntime
	pluginWasm *pluginWasmFetcher
	connectors *connectorRegistry

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

// providerKeys is the owner's decrypted model credentials, which the
// compiler needs to build a working gateway.
type providerKeys interface {
	Keys(ctx context.Context, ownerID int64) (map[string]modelgateway.Credential, error)
}

// NewEngine's pluginRuntime is nil-safe (adk.PluginRuntime, typically
// *internal/adapter/extism.Runtime) — plugins.CompileAgent simply never
// builds a KindPlugin tool because nothing resolves a
// "plugin:{id}/{tool}" capabilities ref to one yet: that resolution (a
// resourceAuthorizer branch reading plugin_installations and fetching the
// installed version's wasm from OSS) is deliberately not part of P1
// (spec-20 §七's own phasing checkpoint) — wiring the runtime through now
// means it's ready the moment that branch lands, without another engine
// signature change.
func NewEngine(
	queries store.Querier, runs run.Repository, events run.EventStore,
	gates run.GateRepository, registry *GateRegistry, keys providerKeys, aesKey []byte,
	kbService *knowledgebase.Service, skillObjectStore resource.ObjectStore, pluginRuntime adk.PluginRuntime,
	connectors *connectorRegistry,
) *Engine {
	return &Engine{
		queries: queries, runs: runs, events: events, gates: gates, registry: registry,
		keys: keys, aesKey: aesKey, appName: "agentic-kit", cancels: map[string]context.CancelFunc{},
		kbSearcher: newKnowledgeBaseSearcher(kbService),
		skills:     newSkillContentFetcher(skillObjectStore),
		plugins:    pluginRuntime,
		// Plugin packages and Skill zips live in the same OSS bucket
		// (spec-20 §3.1 reuses the Skill-upload storage convention
		// verbatim), so the wasm fetcher shares skillObjectStore rather
		// than main.go wiring a second, functionally identical store.
		pluginWasm: newPluginWasmFetcher(skillObjectStore),
		connectors: connectors,
	}
}

var _ run.Orchestrator = (*Engine)(nil)

// Prepare compiles one Bundle for one run: every agents[] entry through
// adk.CompileAgent (each resource reference gated by the authorizer), then
// the whole graph through adk.CompileBundle.
func (e *Engine) Prepare(ctx context.Context, runID string, b run.ResolvedBundle, gateConfigs map[string]run.GateConfig) (run.Execution, error) {
	runType, _ := b.Definition["type"].(string)
	if runType == "" {
		runType = "graph"
	}

	// A flow/single Bundle has no orchestration block to parse at all —
	// only a graph Bundle's edges need bundlegraph's graph shape.
	var graph bundlegraph.Graph
	if runType == "graph" {
		var err error
		graph, err = bundlegraph.ParseGraph(b.Definition)
		if err != nil {
			return nil, fmt.Errorf("parse orchestration graph: %w", err)
		}
	}

	creds, err := e.keys.Keys(ctx, b.OwnerUserID)
	if err != nil {
		return nil, fmt.Errorf("load provider keys: %w", err)
	}
	gateway := modelgateway.NewGateway(nil)
	authorizer := newResourceAuthorizer(ctx, e.queries, b.OwnerUserID, e.aesKey, e.pluginWasm, e.connectors)

	agentsRaw, _ := b.Definition["agents"].([]any)
	compiled := make(map[string]adk.CompiledAgent, len(agentsRaw))
	nodeOrder := make([]string, 0, len(agentsRaw)) // agents[] declaration order — a flow's implicit schedule
	// renderRules is consulted per node once its output/tool calls stream
	// in during Start — a node.render event (spec-20 §4.2) is not
	// something CompileAgent produces, only something it gathers the
	// matching rules for.
	renderRules := make(map[string][]adk.RendererRegistration, len(agentsRaw))
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

		agentDef, err := e.resolveAgentDefinition(ctx, b.OwnerUserID, am, ref, version)
		if err != nil {
			return nil, fmt.Errorf("resolve agent %q: %w", ref, err)
		}
		agentDef["agent"] = node

		var nodeRenderers []adk.RendererRegistration
		compiledAgent, err := adk.CompileAgent(ctx, agentDef, adk.AgentCompileOptions{
			Gateway: gateway, Credentials: creds, Authorizer: authorizer,
			KnowledgeBaseSearcher: e.kbSearcher, SkillContentFetcher: e.skills, PluginRuntime: e.plugins,
			Renderers: &nodeRenderers,
		})
		if len(nodeRenderers) > 0 {
			renderRules[node] = nodeRenderers
		}
		if err != nil {
			return nil, fmt.Errorf("compile agent %q: %w", node, err)
		}
		compiled[node] = compiledAgent
		nodeOrder = append(nodeOrder, node)
	}

	gateNodes := make(map[string]bool, len(gateConfigs))
	for node := range gateConfigs {
		gateNodes[node] = true
	}
	waiter := &gateWaiter{gates: e.gates, events: e.events, registry: e.registry, runID: runID, configs: gateConfigs}

	bundleRef, _ := b.Definition["bundle"].(string)

	// The run type genuinely changes how the Bundle is dispatched, not just
	// how it was authored: graph keeps the hand-rolled graph-walking
	// executor, flow is built on ADK's own SequentialAgent, and single
	// skips compilation altogether.
	var root adk.CompiledAgent
	switch runType {
	case "flow":
		root, err = adk.CompileFlow(adk.FlowCompileOptions{
			BundleRef: bundleRef, Nodes: nodeOrder, Agents: compiled, GateNodes: gateNodes, GateWaiter: waiter,
		})
	case "single":
		root, err = adk.CompileSingle(bundleRef, firstOrEmpty(nodeOrder), compiled)
	default:
		root, err = adk.CompileBundle(adk.BundleCompileOptions{
			BundleRef: bundleRef, Graph: graph, Agents: compiled, GateNodes: gateNodes, GateWaiter: waiter,
		})
	}
	if err != nil {
		return nil, err
	}
	return &execution{engine: e, runID: runID, root: root, ownerID: b.OwnerUserID, renderRules: renderRules, authorizer: authorizer}, nil
}

func firstOrEmpty(nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0]
}

// resolveAgentDefinition picks where an agents[] entry's definition comes
// from. Normally it's a (ref, version) pair resolved against the agent
// registry. An entry may instead carry the definition inline, which is how
// a 草稿试运行 runs an Agent the user is still editing and has never saved
// (run.Service.StartAgentTest) — that shape is constructed server-side
// only; schemas/bundle.schema.json does not let anyone author it into a
// stored Bundle, so a persisted Bundle can never smuggle past agent
// versioning this way.
func (e *Engine) resolveAgentDefinition(ctx context.Context, ownerID int64, entry map[string]any, ref, version string) (map[string]any, error) {
	if inline, ok := entry["definition"].(map[string]any); ok {
		// Copied because Prepare writes the node name into it, and the
		// caller's map may be reused across a retry.
		def := make(map[string]any, len(inline))
		for k, v := range inline {
			def[k] = v
		}
		return def, nil
	}
	return e.loadAgentDefinition(ctx, ownerID, ref, version)
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
	engine  *Engine
	runID   string
	root    adk.CompiledAgent
	ownerID int64
	// renderRules is Prepare's per-node auto_render/tools[].ui registry
	// (spec-20 §4.2) — Start's emit callback consults it after each node
	// event to decide whether a node.render event should follow.
	renderRules map[string][]adk.RendererRegistration
	// authorizer is the same instance Prepare used to compile every node —
	// kept so Start can release whatever connector connections it bound
	// once this run ends (spec-20 §4.5's "谁创建谁回收").
	authorizer *resourceAuthorizer
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
	defer x.authorizer.ReleaseBound()

	persist := context.Background()

	runner, err := adk.NewADKRunner(e.appName, x.root)
	if err != nil {
		x.finish(persist, run.StatusFailed, err.Error())
		return
	}
	// Real, persisted memory when the owner has registered a "memory"
	// resource — otherwise ADKRunner falls back to its own process-local
	// default, same as it always did before 记忆库 existed.
	if row, err := e.queries.GetNewestEnabledMemoryForOwner(ctx, x.ownerID); err == nil {
		runner.MemoryService = adk.NewMemoryService(postgres.NewMemoryStore(e.queries, row.ID, x.ownerID))
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
			x.emitRenderIfMatched(persist, ev)

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

// emitRenderIfMatched implements spec-20 §4.2's two render triggers against
// one already-persisted node event: an explicit tools[].ui call (method A —
// ev.Type is node.tool_call.finished and the tool name matches a
// registration's TriggerTool) or an auto_render fenced-code-block match
// (method B — ev.Type is node.finished and the output text matches a
// registration's FencedLangs, first-declared-wins). Neither trigger fires
// twice for the same underlying event; at most one node.render event
// follows.
func (x *execution) emitRenderIfMatched(ctx context.Context, ev adk.Event) {
	regs := x.renderRules[ev.Node]
	if len(regs) == 0 {
		return
	}

	switch ev.Type {
	case adk.EventNodeToolCallFinished:
		name, _ := ev.Payload["name"].(string)
		reg, ok := adk.MatchToolRender(name, regs)
		if !ok {
			return
		}
		_ = x.engine.events.Append(ctx, run.Event{
			RunID: x.runID, Type: adk.EventNodeRender, Node: ev.Node,
			Payload: map[string]any{
				"plugin": reg.PluginID, "version": reg.Version, "renderer": reg.RendererName,
				"resource_uri": reg.ResourceURI(), "entry": reg.Entry, "data": ev.Payload["result"],
			},
		})
	case adk.EventNodeFinished:
		text, _ := ev.Payload["text"].(string)
		if text == "" {
			return
		}
		reg, lang, content, matched := adk.MatchAutoRender(text, regs)
		if !matched {
			return
		}
		_ = x.engine.events.Append(ctx, run.Event{
			RunID: x.runID, Type: adk.EventNodeRender, Node: ev.Node,
			Payload: map[string]any{
				"plugin": reg.PluginID, "version": reg.Version, "renderer": reg.RendererName,
				"resource_uri": reg.ResourceURI(), "entry": reg.Entry, "data": map[string]any{"lang": lang, "content": content},
			},
		})
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
