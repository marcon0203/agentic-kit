// Package run is the 编排运行时 bounded context: starting a Bundle run,
// reporting on it, stopping it, and resolving the human approval gates it
// blocks on.
//
// Two rules give this context its shape, and both are about what a caller
// is allowed to see:
//
//   - Black-box (spec-08/11). A run started by someone who is not the
//     Bundle's author exposes only the outputs the Bundle declares, and
//     only the events not marked internal. Intermediate nodes routinely
//     write prompt fragments into shared_state; those never leave.
//   - Sanitised failure (spec-11 "错误信息必须脱敏"). A failure never names
//     the private resource that caused it, at pre-flight or mid-run.
//
// Both are enforced here rather than at each handler, so a new endpoint
// cannot forget them.
package run

import "time"

// Status is a run's lifecycle state. There is no separate "cancelled":
// spec-11 records a user stop as a failure with a stopped-by-user message,
// keeping the terminal set at two.
type Status string

const (
	StatusRunning  Status = "running"
	StatusFinished Status = "finished"
	StatusFailed   Status = "failed"
)

// Terminal reports whether no further progress is possible — the condition
// both the cancel endpoint (50002) and the event stream's close check ask
// about.
func (s Status) Terminal() bool { return s == StatusFinished || s == StatusFailed }

// Run is one execution of a Bundle.
type Run struct {
	ID            string
	BundleID      int64
	BundleRef     string
	BundleVersion string
	TriggeredBy   int64
	ViaListingID  *int64
	Status        Status
	Error         string
	SharedState   map[string]any
	Usage         Usage
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// Usage is a run's accumulated cost. DurationSeconds is derived rather
// than stored: it is only meaningful once the run has finished.
type Usage struct {
	TotalTokens int64
	CostUSD     float64
}

// DurationSeconds returns the wall-clock length of a finished run, or 0
// while it is still going.
func (r Run) DurationSeconds() int64 {
	if r.FinishedAt == nil {
		return 0
	}
	return int64(r.FinishedAt.Sub(r.CreatedAt).Seconds())
}

// Detail is the read model behind GET /runs/{id}: a run as one particular
// requester is allowed to see it.
type Detail struct {
	Run         Run
	IsOwner     bool
	SharedState map[string]any
}

// Event is one persisted run event. IsInternal marks the ones only the
// Bundle's author may see.
type Event struct {
	ID         int64
	RunID      string
	Type       string
	Node       string
	Payload    map[string]any
	IsInternal bool
	CreatedAt  time.Time
}

// Lifecycle event types the runtime itself produces. ADK has no event for
// "the graph is about to execute", and spec-14's Chat page needs one to
// leave its starting placeholder — so the runtime owns these three. They
// are never internal: a run's own start and end is not reasoning detail.
const (
	EventBundleStarted  = "bundle.started"
	EventBundleFinished = "bundle.finished"
	EventBundleFailed   = "bundle.failed"
	EventGateWaiting    = "human_gate.waiting"
	EventGateResolved   = "human_gate.resolved"
)

// FilterSharedState keeps only the keys a Bundle declares as outputs.
//
// This is the black-box boundary in one function: a subscriber sees the
// declared output surface and nothing else, so an intermediate node that
// stashed a prompt fragment under some other key cannot leak it. Declared
// keys that the run never wrote are simply absent rather than null.
func FilterSharedState(state map[string]any, declaredOutputs []string) map[string]any {
	out := map[string]any{}
	for _, key := range declaredOutputs {
		if v, ok := state[key]; ok {
			out[key] = v
		}
	}
	return out
}

// Limits mirrors Bundle.limits (schemas/bundle.schema.json) — spec-11's
// global circuit breaker. A zero field means that dimension is unbounded.
type Limits struct {
	MaxTotalTokens      int64
	MaxCostUSD          float64
	MaxWallClockSeconds int64
}

// Breach reports the message to fail a run with once accumulated usage has
// passed a limit, or "" while it is still within them. Returning the
// message rather than a bool keeps the wording — which the user sees as
// bundle_runs.error — beside the rule that produces it.
func (l Limits) Breach(u Usage) string {
	if l.MaxTotalTokens > 0 && u.TotalTokens > l.MaxTotalTokens {
		return "超出 max_total_tokens 限制，运行已终止"
	}
	if l.MaxCostUSD > 0 && u.CostUSD > l.MaxCostUSD {
		return "超出 max_cost_usd 限制，运行已终止"
	}
	return ""
}

// Failure messages for the terminal conditions the runtime detects itself.
const (
	FailWallClockExceeded = "超出 max_wall_clock_seconds 限制，运行已终止"
	FailCancelledByUser   = "运行已被用户停止"
	FailAllProvidersDown  = "所有模型 Provider 均不可用"
	FailGeneric           = "运行执行失败"
)
