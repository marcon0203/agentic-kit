package run

import (
	"context"
	"errors"
)

// Port sentinels. Adapters translate their storage's own signals into
// these; the service never sees a pgx error.
var (
	ErrNotFound      = errors.New("run: not found")
	ErrNotSubscribed = errors.New("run: caller is not subscribed to this listing")
	ErrGateResolved  = errors.New("run: gate already resolved")
)

// Repository persists runs.
type Repository interface {
	Create(ctx context.Context, r Run) (Run, error)
	Get(ctx context.Context, runID string) (Run, error)
	ListPage(ctx context.Context, q ListQuery) ([]Run, error)
	UpdateStatus(ctx context.Context, runID string, status Status, errMsg string) error
	MarkCancelRequested(ctx context.Context, runID string) error
	AddUsage(ctx context.Context, runID string, tokens int64, costUSD float64) error
}

// ListQuery filters GET /runs. Runs paginate by offset rather than keyset:
// the list is filtered on two optional columns and ordered newest-first,
// so there is no single monotonic key to resume from.
type ListQuery struct {
	TriggeredBy int64
	BundleRef   string
	Status      string
	Limit       int
	Offset      int
}

// EventStore appends and reads run events. Excluding internal events is a
// query parameter rather than a post-filter so the black-box subset never
// travels further than it has to.
type EventStore interface {
	Append(ctx context.Context, ev Event) error
	ListAfter(ctx context.Context, runID string, afterID int64, includeInternal bool) ([]Event, error)
}

// ResolvedBundle is the Bundle a run will execute, together with who owns
// it and — when the caller reached it through a subscription — which
// listing bound them to this exact version.
type ResolvedBundle struct {
	BundleID     int64
	Ref          string
	Version      string
	Definition   map[string]any
	OwnerUserID  int64
	ViaListingID *int64
	// DeclaredOutputs is the Bundle's io_description.outputs — the only
	// shared_state keys a non-author may see.
	DeclaredOutputs []string
}

// BundleResolver implements spec-11 steps ②③: a ref the caller owns runs
// directly; otherwise it must be a listing the caller is subscribed to,
// and the version is the one that subscription bound (快照隔离), never a
// caller-supplied one. Returns ErrNotSubscribed when neither holds.
type BundleResolver interface {
	Resolve(ctx context.Context, userID int64, bundleRef, bundleVersion string) (ResolvedBundle, error)
	// LoadForRun re-reads the Bundle behind an existing run, for the read
	// paths that need its ref, version and declared outputs.
	LoadForRun(ctx context.Context, bundleID int64) (ResolvedBundle, error)
}

// DependencyStatus is the outcome of the pre-flight recheck. It names the
// *kind* of unavailability rather than the resource: which resource is
// missing is exactly what spec-11 forbids telling the caller.
type DependencyStatus int

const (
	DependenciesOK DependencyStatus = iota
	DependencyAgentMissing
	DependencyResourceUnavailable
	DependencyProviderMissing
)

// DependencyChecker re-verifies, at launch, that everything the Bundle's
// Agents transitively need still exists, is enabled, and has a configured
// model provider — the author may have disabled something since publishing
// (spec-11 step ④).
type DependencyChecker interface {
	Check(ctx context.Context, ownerID int64, bundleDef map[string]any) (DependencyStatus, error)
}

// ProviderDetailChecker 是 DependencyChecker 的可选补充：说清楚是**哪些**
// 模型提供商没配好。
//
// Check 的报错刻意脱敏（spec-11：订阅者不该知道一个私有 Bundle 是用什么搭
// 的），但试运行自己的 Agent 不存在这个顾虑——那是你自己刚写的东西，含糊
// 的"Provider 未配置"只会让人去猜是哪一个、猜是不是没保存成功。
type ProviderDetailChecker interface {
	MissingProviders(ctx context.Context, ownerID int64, bundleDef map[string]any) ([]string, error)
}

// Orchestrator compiles a Bundle definition into something executable and
// runs it. Compilation and execution are one port because a compiled graph
// is an opaque handle: the domain decides *when* to compile, launch and
// cancel, and never inspects what compilation produced.
type Orchestrator interface {
	// Prepare compiles the Bundle for a specific run. A compilation
	// failure means the Bundle cannot execute as it stands (422).
	Prepare(ctx context.Context, runID string, b ResolvedBundle, gates map[string]GateConfig) (Execution, error)
	// Cancel stops an in-flight run. Reports whether a run was actually
	// executing here to be stopped.
	Cancel(runID string) bool
}

// Execution is one prepared, not-yet-started run.
type Execution interface {
	// Start drives the run to completion. It blocks, so the service
	// launches it in its own goroutine — POST /runs returns as soon as the
	// run_id exists ("异步启动，立即返回 run_id").
	Start(triggeredBy int64, input map[string]any, limits Limits)
}

// GateRepository persists human gates.
type GateRepository interface {
	CreatePending(ctx context.Context, runID string, cfg GateConfig) (Gate, error)
	// FindPending returns ErrNotFound when the node has no pending gate —
	// which, for an approval request, means it was already resolved.
	FindPending(ctx context.Context, runID, node string) (Gate, error)
	// Resolve records a decision. It returns ErrGateResolved if the gate
	// was resolved in the meantime, so a race with the timeout scanner
	// loses cleanly rather than double-resolving.
	Resolve(ctx context.Context, gateID int64, d Decision, resolvedBy *int64) error
	ListPastTimeout(ctx context.Context) ([]Gate, error)
}

// GateNotifier delivers a decision to the goroutine blocked on that gate.
// It reports whether a waiter was actually there — after a restart there
// is none, and the run it belonged to is already lost.
type GateNotifier interface {
	Notify(gateID int64, d Decision) bool
}

// AuditLog is the append-only record spec-11 requires for every approval
// decision, in addition to the gate row itself.
type AuditLog interface {
	Record(ctx context.Context, actorUserID *int64, action, targetType, targetID string, detail map[string]any) error
}
