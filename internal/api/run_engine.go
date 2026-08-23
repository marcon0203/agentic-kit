package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RunEngine implements spec-11's run-creation chain and the async
// execution loop behind it. It's the only place that wires
// internal/orchestrator/adk (the compiler) to internal/modelgateway (the
// completion abstraction) and to persistence.
type RunEngine struct {
	Queries    store.Querier
	AESKey     []byte
	Gates      *gateRegistry
	RefChecker *ResourceRefChecker
	AppName    string

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewRunEngine(q store.Querier, aesKey []byte, gates *gateRegistry, refChecker *ResourceRefChecker) *RunEngine {
	return &RunEngine{
		Queries: q, AESKey: aesKey, Gates: gates, RefChecker: refChecker, AppName: "agentic-kit",
		cancels: map[string]context.CancelFunc{},
	}
}

// runEngineError is a business error the HTTP handler translates directly
// to an envelope — it carries its own HTTP status and error code so the
// engine (which has no notion of http.ResponseWriter) stays testable on
// its own.
type runEngineError struct {
	Status  int
	Code    int
	Message string
}

func (e *runEngineError) Error() string { return e.Message }

func newRunEngineError(status, code int, message string) *runEngineError {
	return &runEngineError{Status: status, Code: code, Message: message}
}

var errInternal = newRunEngineError(500, ErrInternal, "internal server error")

// resolvedBundle is the private Bundle a run will execute, plus who owns
// it and — when the requester reached it via a subscription rather than
// owning it themselves — which listing bound them to this exact version.
type resolvedBundle struct {
	Row          store.Bundle
	OwnerUserID  int64
	ViaListingID *int64
}

// ResolveBundle implements spec-11's chain step ②③: a bundle_ref the
// caller owns runs directly; otherwise it must be a listing_ref the
// caller is subscribed to (403 + 70003 if not — "只校验 listing 存在的话，
// 任何人拿到 ID 就能免订阅运行别人的资源"), and the version used is always
// the one that subscription bound to (快照隔离), never a caller-supplied
// bundle_version.
func (e *RunEngine) ResolveBundle(ctx context.Context, userID int64, bundleRef, bundleVersion string) (resolvedBundle, *runEngineError) {
	var row store.Bundle
	var err error
	if bundleVersion != "" {
		row, err = e.Queries.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{OwnerUserID: userID, BundleRef: bundleRef, Version: bundleVersion})
	} else {
		row, err = e.Queries.GetBundleLatestByRef(ctx, store.GetBundleLatestByRefParams{OwnerUserID: userID, BundleRef: bundleRef})
	}
	if err == nil {
		return resolvedBundle{Row: row, OwnerUserID: userID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return resolvedBundle{}, errInternal
	}

	sub, err := e.Queries.GetSubscriptionForSubscriberByListingRef(ctx, store.GetSubscriptionForSubscriberByListingRefParams{
		SubscriberID: userID, ListingRef: bundleRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedBundle{}, newRunEngineError(403, ErrNotSubscribed, "未订阅该资源，无法运行")
	}
	if err != nil {
		return resolvedBundle{}, errInternal
	}

	listing, err := e.Queries.GetListingByID(ctx, sub.ListingID)
	if err != nil {
		return resolvedBundle{}, errInternal
	}
	if listing.ResourceType != "bundle" {
		return resolvedBundle{}, newRunEngineError(403, ErrNotSubscribed, "未订阅该资源，无法运行")
	}

	bundleRow, err := e.Queries.GetBundleByID(ctx, listing.ResourceID)
	if err != nil {
		return resolvedBundle{}, errInternal
	}
	listingID := listing.ID
	return resolvedBundle{Row: bundleRow, OwnerUserID: bundleRow.OwnerUserID, ViaListingID: &listingID}, nil
}

// CheckDependencies re-verifies every resource the Bundle's Agents
// transitively need still exists, is enabled, and has a configured model
// Provider (spec-11 step ④: "作者可能事后禁用了资源"). Every error message
// is generic — never names the missing/disabled resource — per spec-11's
// explicit "错误信息必须脱敏" requirement.
func (e *RunEngine) CheckDependencies(ctx context.Context, ownerID int64, bundleDef map[string]any, apiKeys map[string]string) *runEngineError {
	agentsRaw, _ := bundleDef["agents"].([]any)
	for _, a := range agentsRaw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := am["ref"].(string)
		version, _ := am["version"].(string)

		agentRow, err := e.resolveAgentVersion(ctx, ownerID, ref, version)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return newRunEngineError(422, ErrAgentVersionNotFound, "该 Bundle 引用的部分资源当前不可用")
			}
			return errInternal
		}

		var agentDef map[string]any
		if err := json.Unmarshal(agentRow.Definition, &agentDef); err != nil {
			return errInternal
		}
		caps, _ := agentDef["capabilities"].(map[string]any)
		tools := toStringSlice(caps["tools"])
		skills := toStringSlice(caps["skills"])
		fieldErrs, err := e.RefChecker.CheckCapabilities(ctx, ownerID, tools, skills)
		if err != nil {
			return errInternal
		}
		if len(fieldErrs) > 0 {
			return newRunEngineError(422, ErrResourceDisabled, "该 Bundle 引用的部分资源当前不可用")
		}

		model, _ := agentDef["model"].(map[string]any)
		provider, _ := model["provider"].(string)
		if provider != "" {
			if _, ok := apiKeys[provider]; !ok {
				if ok := hasFallbackKey(model, apiKeys); !ok {
					return newRunEngineError(422, ErrProviderNotConfigured, "该 Bundle 所需的模型 Provider 未配置")
				}
			}
		}
	}
	return nil
}

func hasFallbackKey(model map[string]any, apiKeys map[string]string) bool {
	fallbacks := toStringSlice(model["fallback"])
	for _, f := range fallbacks {
		spec, err := modelgateway.ParseModelSpec(f)
		if err != nil {
			continue
		}
		if _, ok := apiKeys[spec.Provider]; ok {
			return true
		}
	}
	return false
}

func (e *RunEngine) resolveAgentVersion(ctx context.Context, ownerID int64, ref, version string) (store.Agent, error) {
	if version != "" {
		return e.Queries.GetAgentForOwner(ctx, store.GetAgentForOwnerParams{OwnerUserID: ownerID, AgentRef: ref, Version: version})
	}
	return e.Queries.GetAgentLatestByRef(ctx, store.GetAgentLatestByRefParams{OwnerUserID: ownerID, AgentRef: ref})
}

// DecryptedAPIKeys returns owner's model-provider credentials, provider
// name -> plaintext key. Only the latest row per provider is kept
// (ListModelProvidersForOwner is already ordered newest-first).
func (e *RunEngine) DecryptedAPIKeys(ctx context.Context, ownerID int64) (map[string]string, error) {
	rows, err := e.Queries.ListModelProvidersForOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	for _, row := range rows {
		if _, ok := keys[row.Provider]; ok {
			continue
		}
		encrypted, err := e.Queries.GetModelProviderCredentials(ctx, store.GetModelProviderCredentialsParams{ID: row.ID, OwnerUserID: ownerID})
		if err != nil {
			return nil, err
		}
		plaintext, err := crypto.Decrypt(e.AESKey, string(encrypted))
		if err != nil {
			return nil, err
		}
		keys[row.Provider] = string(plaintext)
	}
	return keys, nil
}

// Compile builds the root ADK agent for one Bundle: every agents[] entry
// via adk.CompileAgent (resource-authorization-gated), then the whole
// graph via adk.CompileBundle.
func (e *RunEngine) Compile(ctx context.Context, runID string, ownerID int64, bundleDef map[string]any, apiKeys map[string]string, gw *modelgateway.Gateway) (adk.CompiledAgent, error) {
	graph, err := bundlegraph.ParseGraph(bundleDef)
	if err != nil {
		return nil, fmt.Errorf("parse orchestration graph: %w", err)
	}

	agentsRaw, _ := bundleDef["agents"].([]any)
	compiled := make(map[string]adk.CompiledAgent, len(agentsRaw))
	authorizer := newStoreResourceAuthorizer(ctx, e.Queries, ownerID, e.AESKey)
	for _, a := range agentsRaw {
		am, _ := a.(map[string]any)
		ref, _ := am["ref"].(string)
		alias, _ := am["alias"].(string)
		version, _ := am["version"].(string)
		node := ref
		if alias != "" {
			node = alias
		}

		agentRow, err := e.resolveAgentVersion(ctx, ownerID, ref, version)
		if err != nil {
			return nil, fmt.Errorf("resolve agent %q: %w", ref, err)
		}
		var agentDef map[string]any
		if err := json.Unmarshal(agentRow.Definition, &agentDef); err != nil {
			return nil, err
		}
		agentDef["agent"] = node // compile under its Bundle-local node name (ref or alias)

		compiledAgent, err := adk.CompileAgent(ctx, agentDef, adk.AgentCompileOptions{Gateway: gw, APIKeys: apiKeys, Authorizer: authorizer})
		if err != nil {
			return nil, fmt.Errorf("compile agent %q: %w", node, err)
		}
		compiled[node] = compiledAgent
	}

	gateNodes, gateConfigs := parseHumanGates(bundleDef)
	waiter := newRunGateWaiter(e.Queries, e.Gates, runID, gateConfigs)

	bundleRef, _ := bundleDef["bundle"].(string)
	root, err := adk.CompileBundle(adk.BundleCompileOptions{
		BundleRef: bundleRef, Graph: graph, Agents: compiled, GateNodes: gateNodes, GateWaiter: waiter,
	})
	if err != nil {
		return nil, err
	}
	return root, nil
}

func parseHumanGates(bundleDef map[string]any) (map[string]bool, map[string]gateNodeConfig) {
	orch, _ := bundleDef["orchestration"].(map[string]any)
	gatesRaw, _ := orch["human_gates"].([]any)
	nodes := map[string]bool{}
	configs := map[string]gateNodeConfig{}
	for _, g := range gatesRaw {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		after, _ := gm["after"].(string)
		if after == "" {
			continue
		}
		nodes[after] = true
		cfg := gateNodeConfig{OnTimeout: "abort", ApproverRoles: toStringSlice(gm["approver_roles"])}
		if ts, ok := gm["timeout_seconds"].(float64); ok {
			v := int32(ts)
			cfg.TimeoutSeconds = &v
		}
		if ot, ok := gm["on_timeout"].(string); ok && ot != "" {
			cfg.OnTimeout = ot
		}
		configs[after] = cfg
	}
	return nodes, configs
}

// runLimits mirrors Bundle.limits (schemas/bundle.schema.json) — the
// global circuit breaker spec-11 requires.
type runLimits struct {
	MaxTotalTokens      int64
	MaxCostUSD          float64
	MaxWallClockSeconds int64
}

func parseRunLimits(bundleDef map[string]any) runLimits {
	l, _ := bundleDef["limits"].(map[string]any)
	var out runLimits
	if v, ok := l["max_total_tokens"].(float64); ok {
		out.MaxTotalTokens = int64(v)
	}
	if v, ok := l["max_cost_usd"].(float64); ok {
		out.MaxCostUSD = v
	}
	if v, ok := l["max_wall_clock_seconds"].(float64); ok {
		out.MaxWallClockSeconds = int64(v)
	}
	return out
}

// Execute drives one compiled Bundle to completion, persisting every
// translated event (spec-10 §3 / spec-11 step ⑥) and accumulating usage
// (step ⑦) as it goes, enforcing the global circuit breaker, and updating
// bundle_runs' terminal status. It's meant to run in its own goroutine —
// CreateRun launches it and returns immediately (openapi: "异步启动，立即
// 返回 run_id").
func (e *RunEngine) Execute(runID string, root adk.CompiledAgent, userID int64, input map[string]any, limits runLimits) {
	ctx := context.Background()
	if limits.MaxWallClockSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(limits.MaxWallClockSeconds)*time.Second)
		defer cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	e.registerCancel(runID, cancel)
	defer e.unregisterCancel(runID)
	defer cancel()

	runner, err := adk.NewADKRunner(e.AppName, root)
	if err != nil {
		e.finishRun(context.Background(), runID, "failed", err.Error())
		return
	}

	msg, err := json.Marshal(input)
	if err != nil {
		msg = []byte("{}")
	}

	// bundle.started is a run-lifecycle signal the run engine owns
	// directly — ADK has no event of its own for "the graph is about to
	// execute" (see events.go's package comment), and spec-14's Chat page
	// uses it to switch out of its initial "正在启动…" placeholder state.
	e.persistLifecycleEvent(context.Background(), runID, "bundle.started", nil)

	var totalTokens int64
	var totalCostUSD float64
	breached := ""

	_, runErr := runner.Run(ctx, adk.AgentInput{UserID: fmt.Sprint(userID), SessionID: runID, Message: string(msg)}, func(ev adk.Event) {
		e.persistEvent(context.Background(), runID, ev)

		if ev.InputTokens != 0 || ev.OutputTokens != 0 || ev.CostUSD != 0 {
			tokens := ev.InputTokens + ev.OutputTokens
			totalTokens += tokens
			totalCostUSD += ev.CostUSD
			_ = modelgateway.RecordUsage(context.Background(), e.Queries, runID, tokens, ev.CostUSD)

			if limits.MaxTotalTokens > 0 && totalTokens > limits.MaxTotalTokens {
				breached = "超出 max_total_tokens 限制，运行已终止"
			}
			if limits.MaxCostUSD > 0 && totalCostUSD > limits.MaxCostUSD {
				breached = "超出 max_cost_usd 限制，运行已终止"
			}
			if breached != "" {
				cancel()
			}
		}
	})

	switch {
	case breached != "":
		e.finishRun(context.Background(), runID, "failed", breached)
	case runErr != nil && errors.Is(runErr, context.DeadlineExceeded):
		e.finishRun(context.Background(), runID, "failed", "超出 max_wall_clock_seconds 限制，运行已终止")
	case runErr != nil && errors.Is(runErr, context.Canceled):
		e.finishRun(context.Background(), runID, "failed", "运行已被用户停止")
	case runErr != nil:
		e.finishRun(context.Background(), runID, "failed", sanitizeRunError(runErr))
	default:
		e.finishRun(context.Background(), runID, "finished", "")
	}
}

// sanitizeRunError strips anything that might name a private resource
// from an execution-time failure before it's stored as bundle_runs.error
// — spec-11's "错误信息必须脱敏" applies just as much mid-run as it does
// at the pre-flight dependency check.
func sanitizeRunError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "modelgateway: all providers") {
		return "所有模型 Provider 均不可用"
	}
	return "运行执行失败"
}

func (e *RunEngine) persistEvent(ctx context.Context, runID string, ev adk.Event) {
	payload, _ := json.Marshal(ev.Payload)
	_, _ = e.Queries.InsertBundleRunEvent(ctx, store.InsertBundleRunEventParams{
		RunID: runID, Type: ev.Type, Node: pgtype.Text{String: ev.Node, Valid: ev.Node != ""},
		Payload: payload, IsInternal: ev.IsInternal,
	})
}

// persistLifecycleEvent is persistEvent for the run-level (not node-scoped)
// signals the run engine itself produces — bundle.started/finished/failed.
// Always external: a run's own start/end is never an internal-reasoning
// detail, so the black-box filter (spec-08) never needs to hide it.
func (e *RunEngine) persistLifecycleEvent(ctx context.Context, runID, eventType string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	_, _ = e.Queries.InsertBundleRunEvent(ctx, store.InsertBundleRunEventParams{
		RunID: runID, Type: eventType, Payload: body, IsInternal: false,
	})
}

func (e *RunEngine) finishRun(ctx context.Context, runID, status, errMsg string) {
	params := store.UpdateBundleRunStatusParams{
		ID: runID, Status: status, FinishedAt: pgtype.Timestamptz{Valid: true, Time: time.Now()},
	}
	eventType := "bundle.finished"
	payload := map[string]any(nil)
	if errMsg != "" {
		params.Error = pgtype.Text{String: errMsg, Valid: true}
		eventType = "bundle.failed"
		payload = map[string]any{"error": errMsg}
	}
	_ = e.Queries.UpdateBundleRunStatus(ctx, params)
	e.persistLifecycleEvent(ctx, runID, eventType, payload)
}

func (e *RunEngine) registerCancel(runID string, cancel context.CancelFunc) {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	e.cancels[runID] = cancel
}

func (e *RunEngine) unregisterCancel(runID string) {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	delete(e.cancels, runID)
}

// Cancel implements POST /runs/{id}/cancel's "5 秒内生效" — it cancels the
// run's context in-process; already-produced output/events/usage are kept
// as-is (spec-11), only forward progress stops.
func (e *RunEngine) Cancel(runID string) bool {
	e.cancelMu.Lock()
	cancel, ok := e.cancels[runID]
	e.cancelMu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}
