package run

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// IDGenerator produces run ids. A port because the format is a contract
// detail ("run-" + 16 hex chars, per api/openapi.yaml's example) but the
// randomness behind it is not the domain's business.
type IDGenerator interface {
	NewRunID() (string, error)
	// NewSessionID 产生一段新对话的 id，调用方没指定 session_id 时用。
	NewSessionID() (string, error)
}

// Service is the 编排运行时 application service.
type Service struct {
	runs     Repository
	events   EventStore
	bundles  BundleResolver
	deps     DependencyChecker
	orch     Orchestrator
	gates    GateRepository
	notifier GateNotifier
	audit    AuditLog
	ids      IDGenerator

	// testBundles backs StartAgentTest. nil is fine — that surface then
	// returns a clear "not configured" error instead of panicking, the
	// same shape every other optional collaborator on this codebase uses.
	testBundles AgentTestBundleProvider
}

func NewService(
	runs Repository, events EventStore, bundles BundleResolver, deps DependencyChecker,
	orch Orchestrator, gates GateRepository, notifier GateNotifier, audit AuditLog, ids IDGenerator,
) *Service {
	return &Service{runs: runs, events: events, bundles: bundles, deps: deps, orch: orch, gates: gates, notifier: notifier, audit: audit, ids: ids}
}

// WithAgentTestRuns enables StartAgentTest — an opt-in builder step rather
// than a tenth NewService parameter, matching resource.Service's
// WithSkillUploads / WithOpenAPIImport.
func (s *Service) WithAgentTestRuns(testBundles AgentTestBundleProvider) *Service {
	s.testBundles = testBundles
	return s
}

// StartCommand launches a run.
type StartCommand struct {
	BundleRef     string
	BundleVersion string
	Input         map[string]any
	// SessionID 接上已有的一段对话；留空就开一段新的。前端连着发消息时
	// 把上一次拿到的 session_id 带回来，模型才接得住上文。
	SessionID string
}

// AgentTestBundleProvider hands back the placeholder Bundle a 草稿试运行
// hangs off. bundle_runs.bundle_id is a NOT NULL foreign key, so even a run
// that executes an unsaved Agent needs *some* bundle row to point at; the
// adapter lazily creates one hidden system Bundle per owner and returns it
// every time after that. Its stored definition is a placeholder and is
// never executed — StartAgentTest overrides it with the draft.
type AgentTestBundleProvider interface {
	Ensure(ctx context.Context, ownerID int64) (bundleID int64, ref, version string, err error)
}

// AgentTestCommand runs one Agent definition — typically one the caller is
// still editing and has never saved — without going through a Bundle.
type AgentTestCommand struct {
	Definition map[string]any
	Input      map[string]any
	// SessionID 同 StartCommand.SessionID：试运行面板里连着问几句，靠它
	// 串成一段对话。
	SessionID string
}

// StartAgentTest is Start for a single, possibly unsaved, Agent: the 智能体
// 工作台's right-hand test panel. Everything after bundle resolution is
// deliberately the same chain — the same dependency pre-flight, the same
// compiler, the same run row, the same event stream — so what the test
// panel shows is what a real run does, not a second execution path that can
// drift away from it.
//
// The Bundle wrapped around the draft is a "single" one, which is a real
// run type the engine already implements (spec-05a/§bundle run types), not
// a special case invented for testing.
func (s *Service) StartAgentTest(ctx context.Context, userID int64, cmd AgentTestCommand) (Run, error) {
	if s.testBundles == nil {
		return Run{}, domain.Invalid(domain.CodeValidationFailed, "agent test runs are not configured on this deployment")
	}
	agentRef, _ := cmd.Definition["agent"].(string)
	if agentRef == "" {
		return Run{}, domain.Invalid(domain.CodeValidationFailed, "invalid request").
			WithDetails(domain.FieldError{Field: "definition.agent", Reason: "required"})
	}
	if cmd.Input == nil {
		cmd.Input = map[string]any{}
	}

	bundleID, ref, version, err := s.testBundles.Ensure(ctx, userID)
	if err != nil {
		return Run{}, domain.Internal(err)
	}

	resolved := ResolvedBundle{
		BundleID: bundleID, Ref: ref, Version: version, OwnerUserID: userID,
		Definition: map[string]any{
			"bundle": ref,
			"type":   "single",
			// The draft travels inline rather than by ref — see
			// Engine.resolveAgentDefinition, which is the only place that
			// shape is ever produced or consumed.
			"agents": []any{map[string]any{"ref": agentRef, "definition": cmd.Definition}},
		},
	}

	status, err := s.deps.Check(ctx, userID, resolved.Definition)
	if err != nil {
		return Run{}, domain.Internal(err)
	}
	if depErr := s.agentTestDependencyError(ctx, userID, resolved.Definition, status); depErr != nil {
		return Run{}, depErr
	}

	runID, err := s.ids.NewRunID()
	if err != nil {
		return Run{}, domain.Internal(err)
	}
	sessionID, err := s.resolveSessionID(cmd.SessionID)
	if err != nil {
		return Run{}, err
	}

	execution, err := s.orch.Prepare(ctx, runID, resolved, ParseGateConfigs(resolved.Definition))
	if err != nil {
		return Run{}, domain.Unprocessable(domain.CodeAgentVersionNotFound, "该智能体当前无法编译执行").WithCause(err)
	}

	created, err := s.runs.Create(ctx, Run{
		ID: runID, BundleID: bundleID, BundleRef: ref, BundleVersion: version,
		TriggeredBy: userID, Status: StatusRunning, SessionID: sessionID,
	})
	if err != nil {
		return Run{}, domain.Internal(err)
	}

	go execution.Start(userID, sessionID, cmd.Input, ParseLimits(resolved.Definition))
	return created, nil
}

// resolveSessionID 决定这次运行挂在哪段对话上：调用方带了 session_id 就
// 接上去，没带就开一段新的。
//
// 这里**不校验**传进来的 session_id 是否已存在：ADK 的 runner 配的是
// AutoCreateSession，第一次用到一个 id 就自己把会话建出来，前端因此可以
// 先自己生成一个 id 再连着发几条消息，不必先跑一次空运行去换 id。越权也
// 不是这里的问题——会话在存储层是按 (app_name, user_id, session_id) 定位
// 的，user_id 来自登录态，猜到别人的 session_id 也读不到别人的会话。
func (s *Service) resolveSessionID(requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested), nil
	}
	id, err := s.ids.NewSessionID()
	if err != nil {
		return "", domain.Internal(err)
	}
	return id, nil
}

// Start implements spec-11's launch chain: resolve the Bundle (ownership
// or subscription, snapshot-isolated) → recheck dependencies with
// sanitised errors → compile → persist the run → hand it to the
// orchestrator and return immediately.
//
// The order matters and is the reason this lives in one method: every
// check that can reject the request happens before a bundle_runs row
// exists, so a rejected launch leaves no half-started run behind.
func (s *Service) Start(ctx context.Context, userID int64, cmd StartCommand) (Run, error) {
	if cmd.BundleRef == "" {
		return Run{}, domain.Invalid(domain.CodeValidationFailed, "invalid request").
			WithDetails(domain.FieldError{Field: "bundle_ref", Reason: "required"})
	}
	if cmd.Input == nil {
		cmd.Input = map[string]any{}
	}

	resolved, err := s.bundles.Resolve(ctx, userID, cmd.BundleRef, cmd.BundleVersion)
	if err != nil {
		if errors.Is(err, ErrNotSubscribed) || errors.Is(err, ErrNotFound) {
			return Run{}, domain.Forbidden(domain.CodeNotSubscribed, "未订阅该资源，无法运行")
		}
		return Run{}, domain.Internal(err)
	}

	status, err := s.deps.Check(ctx, resolved.OwnerUserID, resolved.Definition)
	if err != nil {
		return Run{}, domain.Internal(err)
	}
	if depErr := dependencyError(status); depErr != nil {
		return Run{}, depErr
	}

	runID, err := s.ids.NewRunID()
	if err != nil {
		return Run{}, domain.Internal(err)
	}
	sessionID, err := s.resolveSessionID(cmd.SessionID)
	if err != nil {
		return Run{}, err
	}

	execution, err := s.orch.Prepare(ctx, runID, resolved, ParseGateConfigs(resolved.Definition))
	if err != nil {
		return Run{}, domain.Unprocessable(domain.CodeAgentVersionNotFound, "该 Bundle 当前无法编译执行").WithCause(err)
	}

	created, err := s.runs.Create(ctx, Run{
		ID: runID, BundleID: resolved.BundleID, BundleRef: resolved.Ref, BundleVersion: resolved.Version,
		TriggeredBy: userID, ViaListingID: resolved.ViaListingID, Status: StatusRunning, SessionID: sessionID,
	})
	if err != nil {
		return Run{}, domain.Internal(err)
	}

	go execution.Start(userID, sessionID, cmd.Input, ParseLimits(resolved.Definition))
	return created, nil
}

// dependencyError turns a pre-flight verdict into the response the caller
// gets. Every message is generic by construction — spec-11's "错误信息必须
// 脱敏": naming the disabled resource would tell a subscriber what a
// private Bundle is built from.
func dependencyError(status DependencyStatus) error {
	switch status {
	case DependencyAgentMissing:
		return domain.Unprocessable(domain.CodeAgentVersionNotFound, "该 Bundle 引用的部分资源当前不可用")
	case DependencyResourceUnavailable:
		return domain.Unprocessable(domain.CodeResourceDisabled, "该 Bundle 引用的部分资源当前不可用")
	case DependencyProviderMissing:
		return domain.Unprocessable(domain.CodeProviderNotConfigured, "该 Bundle 所需的模型 Provider 未配置")
	default:
		return nil
	}
}

// agentTestDependencyError 是 dependencyError 在试运行路径上的版本：缺凭据
// 时把具体是哪个提供商说出来。试运行的对象是调用者自己刚写的 Agent，不适
// 用 spec-11 那条"不能让订阅者知道 Bundle 用了什么"的脱敏约束，而含糊的
// "Provider 未配置"只会让人去猜是哪一个、猜是不是没保存成功。
func (s *Service) agentTestDependencyError(ctx context.Context, userID int64, def map[string]any, status DependencyStatus) error {
	if status != DependencyProviderMissing {
		return dependencyError(status)
	}
	detail, ok := s.deps.(ProviderDetailChecker)
	if !ok {
		return dependencyError(status)
	}
	missing, err := detail.MissingProviders(ctx, userID, def)
	if err != nil || len(missing) == 0 {
		return dependencyError(status)
	}
	return domain.Unprocessable(domain.CodeProviderNotConfigured,
		"模型提供商 "+strings.Join(missing, "、")+" 还没有配置 API Key，去 系统配置 → 模型提供商 里补上")
}

// List returns the caller's own runs, newest first.
// ListSession 返回一段对话里的全部运行，时间正序。
//
// 只按 (调用者, session_id) 查，所以猜到别人的 session_id 也读不到别人的
// 对话；一个陌生的 id 得到的是空列表而不是 404——它可能是前端自己刚生成、
// 还没跑过任何一次运行的新会话。
func (s *Service) ListSession(ctx context.Context, userID int64, sessionID string) ([]Run, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, domain.Invalid(domain.CodeValidationFailed, "invalid request").
			WithDetails(domain.FieldError{Field: "session_id", Reason: "required"})
	}
	rows, err := s.runs.ListInSession(ctx, userID, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Run{}
	}
	return rows, nil
}

func (s *Service) List(ctx context.Context, userID int64, q ListQuery) (domain.Page[Run], error) {
	limit := domain.PageQuery{Limit: q.Limit}.Normalize().Limit
	q.TriggeredBy, q.Limit = userID, limit+1

	rows, err := s.runs.ListPage(ctx, q)
	if err != nil {
		return domain.Page[Run]{}, domain.Internal(err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := domain.Page[Run]{Items: rows, HasMore: hasMore}
	if page.Items == nil {
		page.Items = []Run{}
	}
	if hasMore {
		page.NextCursor = strconv.Itoa(q.Offset + limit)
	}
	return page, nil
}

// Get returns a run as this requester may see it: an author sees the whole
// shared_state, anyone else sees only the Bundle's declared outputs.
//
// The filter keys off "requester is not the Bundle's author" rather than
// "ran via a listing" — someone can reach a Bundle by subscription and
// still be handed the run id, and the author's private intermediate state
// is no less private in that case.
func (s *Service) Get(ctx context.Context, userID int64, runID string) (Detail, error) {
	r, bundle, err := s.load(ctx, userID, runID)
	if err != nil {
		return Detail{}, err
	}

	isOwner := bundle.OwnerUserID == userID
	state := r.SharedState
	if state == nil {
		state = map[string]any{}
	}
	if !isOwner {
		state = FilterSharedState(state, bundle.DeclaredOutputs)
	}
	r.BundleRef, r.BundleVersion = bundle.Ref, bundle.Version
	return Detail{Run: r, IsOwner: isOwner, SharedState: state}, nil
}

// EventsAfter returns the run's events past afterID, restricted to what
// this requester may see.
func (s *Service) EventsAfter(ctx context.Context, userID int64, runID string, afterID int64) ([]Event, error) {
	_, bundle, err := s.load(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	events, err := s.events.ListAfter(ctx, runID, afterID, bundle.OwnerUserID == userID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return events, nil
}

// Status reports a run's current lifecycle state — what the event stream
// re-checks each poll to know when to close.
func (s *Service) Status(ctx context.Context, runID string) (Status, error) {
	r, err := s.runs.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", domain.NotFound(domain.CodeRunNotFound, "run not found")
		}
		return "", domain.Internal(err)
	}
	return r.Status, nil
}

// Cancel implements POST /runs/{id}/cancel.
func (s *Service) Cancel(ctx context.Context, userID int64, runID string) error {
	r, _, err := s.load(ctx, userID, runID)
	if err != nil {
		return err
	}
	if r.Status.Terminal() {
		return domain.Conflict(domain.CodeRunAlreadyFinished, "运行已结束")
	}
	if err := s.runs.MarkCancelRequested(ctx, runID); err != nil {
		return domain.Internal(err)
	}
	s.orch.Cancel(runID)
	return nil
}

// load fetches a run and its Bundle, enforcing that the caller triggered
// it. A run belonging to someone else is reported as not-found rather than
// forbidden: confirming that a run id exists would already leak that
// somebody ran something.
func (s *Service) load(ctx context.Context, userID int64, runID string) (Run, ResolvedBundle, error) {
	r, err := s.runs.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Run{}, ResolvedBundle{}, domain.NotFound(domain.CodeRunNotFound, "run not found")
		}
		return Run{}, ResolvedBundle{}, domain.Internal(err)
	}
	if r.TriggeredBy != userID {
		return Run{}, ResolvedBundle{}, domain.NotFound(domain.CodeRunNotFound, "run not found")
	}
	bundle, err := s.bundles.LoadForRun(ctx, r.BundleID)
	if err != nil {
		return Run{}, ResolvedBundle{}, domain.Internal(err)
	}
	return r, bundle, nil
}

// ResolveGateCommand is one human approval decision.
type ResolveGateCommand struct {
	Node     string
	Approved bool
	Comment  string
}

// ResolveGate implements POST /runs/{id}/gate.
//
// spec-11 requires a second check of the approver's role. V1 has no
// team/role system, so the only person who can ever resolve a gate is the
// user who triggered the run — the same reasoning recorded against
// human_gates.approver_roles in migrations/0010. Roles parsed off the
// Bundle are stored for when that system exists; they are not yet an
// authorisation input, and pretending otherwise would let a Bundle author
// widen who may approve.
func (s *Service) ResolveGate(ctx context.Context, userID int64, runID string, cmd ResolveGateCommand) error {
	if cmd.Node == "" {
		return domain.Invalid(domain.CodeValidationFailed, "node is required")
	}

	r, err := s.runs.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeRunNotFound, "run not found")
		}
		return domain.Internal(err)
	}
	if r.TriggeredBy != userID {
		return domain.Forbidden(domain.CodeGateApproverForbidden, "非指定审批角色")
	}
	if r.Status.Terminal() {
		return domain.Conflict(domain.CodeRunAlreadyFinished, "run 已结束")
	}

	gate, err := s.gates.FindPending(ctx, runID, cmd.Node)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Conflict(domain.CodeGateAlreadyResolved, "gate 已被处理")
		}
		return domain.Internal(err)
	}

	decision := Decision{Status: GateStatusRejected, Approved: cmd.Approved, Reason: "rejected by user"}
	if cmd.Approved {
		decision.Status, decision.Reason = GateStatusApproved, ""
	}
	if cmd.Comment != "" {
		decision.Reason = cmd.Comment
	}

	if err := s.gates.Resolve(ctx, gate.ID, decision, &userID); err != nil {
		if errors.Is(err, ErrGateResolved) {
			// Lost a race with the timeout scanner or a concurrent call.
			return domain.Conflict(domain.CodeGateAlreadyResolved, "gate 已被处理")
		}
		return domain.Internal(err)
	}

	s.recordGateDecision(ctx, gate, decision, &userID, map[string]any{
		"run_id": runID, "node": cmd.Node, "approved": cmd.Approved, "comment": cmd.Comment,
	})
	s.notifier.Notify(gate.ID, decision)
	return nil
}

// ResolveTimedOutGates applies each pending gate's on_timeout policy. The
// scanner exists because a gate must not hold a goroutine hostage: it
// resolves the row and notifies the waiter through exactly the same path
// an explicit approval takes, so the run unblocks identically either way.
func (s *Service) ResolveTimedOutGates(ctx context.Context) error {
	pending, err := s.gates.ListPastTimeout(ctx)
	if err != nil {
		return domain.Internal(err)
	}
	for _, gate := range pending {
		decision := gate.OnTimeout.Apply()
		decision.Reason = "human gate timed out (" + string(gate.OnTimeout) + ")"

		if err := s.gates.Resolve(ctx, gate.ID, decision, nil); err != nil {
			// Resolved by a human in the meantime — nothing to do.
			continue
		}
		s.recordGateDecision(ctx, gate, decision, nil, map[string]any{
			"run_id": gate.RunID, "node": gate.Node, "on_timeout": string(gate.OnTimeout),
		})
		s.notifier.Notify(gate.ID, decision)
	}
	return nil
}

// recordGateDecision writes the two records every resolution leaves: the
// append-only audit entry spec-11 asks for, and the stream event spec-14's
// gate card listens for. Neither failure is worth undoing a decision the
// gate row already carries, so both are best-effort.
func (s *Service) recordGateDecision(ctx context.Context, gate Gate, d Decision, actor *int64, detail map[string]any) {
	_ = s.audit.Record(ctx, actor, "human_gate."+string(d.Status), "human_gate", strconv.FormatInt(gate.ID, 10), detail)

	payload := map[string]any{"gate_id": gate.ID, "status": string(d.Status)}
	if comment, ok := detail["comment"].(string); ok && comment != "" {
		payload["comment"] = comment
	}
	if onTimeout, ok := detail["on_timeout"].(string); ok {
		payload["on_timeout"] = onTimeout
	}
	_ = s.events.Append(ctx, Event{RunID: gate.RunID, Type: EventGateResolved, Node: gate.Node, Payload: payload})
}
