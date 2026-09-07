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
	// SessionID 是这次运行所属的那段对话。同一段对话里的多次运行共享它，
	// 模型因此看得到上文。历史数据可能为空——那些运行本来就各自独立。
	SessionID   string
	Status      Status
	Error       string
	SharedState map[string]any
	Usage       Usage
	CreatedAt   time.Time
	FinishedAt  *time.Time
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

// Event is one persisted run event.
//
// 这里曾经有一个 IsInternal 字段，用来把 node.thinking / node.reasoning
// 挡在非作者身份的事件流之外。它已经删掉了：node.thinking 的内容就是
// node.finished 的前缀，挡掉它保护不了任何东西，只是让订阅者和访客的运行
// 失去流式输出。黑盒边界由 FilterSharedState 和"订阅者读不到 Bundle 定义"
// 守住——那两处才是编排结构与提示词真正会漏出去的地方。
type Event struct {
	ID        int64
	RunID     string
	Type      string
	Node      string
	Payload   map[string]any
	CreatedAt time.Time
	// Ephemeral 标记"这条只走实时推送，不进数据库"。
	//
	// 逐 token 的增量属于传输层，不属于历史：一次运行能产生上千条
	// {"text":"可"} 这样的行，而回放时它们一个字都不贡献——节点跑完后
	// node.finished 携带的是完整正文，timeline 对它是赋值不是追加。历史
	// 是「消息」，不是「token」。
	//
	// 这种事件没有数据库 id（ID 恒为 0），因此不能用来推进断线重连的游标；
	// 中途接入的客户端靠 EventNodeSnapshot 拿到"到目前为止的文字"。
	Ephemeral bool
}

// Lifecycle event types the runtime itself produces. ADK has no event for
// "the graph is about to execute", and spec-14's Chat page needs one to
// leave its starting placeholder — so the runtime owns these three.
const (
	EventBundleStarted  = "bundle.started"
	EventBundleFinished = "bundle.finished"
	EventBundleFailed   = "bundle.failed"
	EventGateWaiting    = "human_gate.waiting"
	EventGateResolved   = "human_gate.resolved"
)

// 节点事件类型。字符串本身是对外契约（见 api/openapi.yaml 的 RunEvent.type
// 与前端 timeline.ts），编排层 internal/orchestrator/adk 各自也有一份同名
// 常量——那一层在依赖方向上位于领域之下，不能反过来 import 这里。两处的值
// 必须一致，改动时一起改。
const (
	EventNodeThinking  = "node.thinking"
	EventNodeReasoning = "node.reasoning"
	EventNodeFinished  = "node.finished"

	// EventNodeSnapshot 是"到目前为止这个节点已经生成的文字"，一条顶替
	// 上千条增量。它只在两种时刻出现：客户端中途接入一次正在跑的运行
	// （刷新页面、断线重连），以及运行以失败告终、增量是仅存的部分答案时
	// 落库的那一条。
	//
	// 前端对它是**赋值**语义（不是像增量那样追加），所以重连时不会把已经
	// 显示的文字再拼一遍。
	EventNodeSnapshot = "node.snapshot"
)

// IsEphemeralType 报告某个事件类型是否只走实时推送、不进数据库。
func IsEphemeralType(t string) bool {
	return t == EventNodeThinking || t == EventNodeReasoning
}

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
