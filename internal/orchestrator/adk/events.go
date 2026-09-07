package adk

import (
	"google.golang.org/adk/session"
)

// Platform event types, mirroring api/openapi.yaml's RunEvent.type enum.
// Only the node-scoped types this package can derive from a raw ADK
// session.Event are produced here — bundle.*, node.queued/started, and
// human_gate.* are run-lifecycle signals the graph executor (this
// package) or the run engine (spec-11, which owns bundle_run_events
// persistence) emit directly, since ADK has no event of its own for "a
// node is about to start" or "a human gate is waiting".
const (
	EventNodeThinking = "node.thinking"
	// EventNodeReasoning is the model's actual chain-of-thought/reasoning
	// trace (a thinking-capable provider's distinct reasoning channel —
	// modelgateway.StreamDelta.ReasoningDelta / CompletionResult.Reasoning),
	// not to be confused with EventNodeThinking, which is really just
	// "streamed answer text before the turn is final" (a typewriter
	// effect). Kept separate so the frontend can render genuine reasoning
	// distinctly instead of it being indistinguishable from partial answer
	// text.
	EventNodeReasoning        = "node.reasoning"
	EventNodeToolCallStarted  = "node.tool_call.started"
	EventNodeToolCallFinished = "node.tool_call.finished"
	EventNodeFinished         = "node.finished"
	// EventNodeRender is not produced by TranslateEvent — the run engine
	// emits it itself (spec-20 §4.2) after matching a node.finished or
	// node.tool_call.finished event's payload against the node's compiled
	// RendererRegistrations. It lives in this const block anyway: it's
	// still a node-scoped platform event in the same "node.*" family, and
	// the frontend's timeline switches on this exact string.
	EventNodeRender = "node.render"
)

// PlatformEvent is the translated shape spec-10 requires: "ADK 产出自己的
// 执行事件，平台订阅后翻译成 POC 已验证的事件模型...保留这层翻译而不是直接
// 透传 ADK 事件——前端契约不应该跟着上游框架的版本演进走". Node is the
// graph node (Bundle.agents[] ref) the event belongs to.
type PlatformEvent struct {
	Type    string
	Node    string
	Payload map[string]any
	// InputTokens/OutputTokens/CostUSD are non-zero only on events derived
	// from a raw session.Event that actually carried usage metadata (an
	// LLM turn) — the run engine accumulates these onto
	// bundle_runs.total_tokens/cost_usd (spec-09/11) without needing its
	// own second lookup into modelgateway's pricing table.
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
}

// TranslateEvent converts one ADK session.Event, produced while executing
// node, into the platform events it corresponds to. A single ADK event can
// translate to more than one platform event (e.g. a response that both
// calls a tool and carries reasoning text).
//
// 每一种事件对所有身份一视同仁。这里曾经给 node.thinking / node.reasoning
// 标 IsInternal，让黑盒订阅者的事件流里没有它们；代价是那些身份下的运行
// 完全没有流式输出——增量文本被挡掉后，只剩最后一条 node.finished 携带整
// 段答案。挡它本来也保护不了什么：node.thinking 就是那段答案的前缀，订阅
// 者过一会儿照样会整段收到。黑盒该守的是 shared_state 的输出面和 Bundle
// 定义本身，不是模型正在往外写的那半句话。
func TranslateEvent(node string, ev *session.Event) []PlatformEvent {
	if ev == nil {
		return nil
	}

	var inputTokens, outputTokens int64
	var costUSD float64
	if ev.UsageMetadata != nil {
		inputTokens = int64(ev.UsageMetadata.PromptTokenCount)
		outputTokens = int64(ev.UsageMetadata.CandidatesTokenCount)
	}
	if cost, ok := ev.CustomMetadata["cost_usd"].(float64); ok {
		costUSD = cost
	}
	withUsage := func(pe PlatformEvent) PlatformEvent {
		pe.InputTokens, pe.OutputTokens, pe.CostUSD = inputTokens, outputTokens, costUSD
		return pe
	}

	var out []PlatformEvent
	if ev.Content != nil {
		for _, part := range ev.Content.Parts {
			if part == nil {
				continue
			}
			switch {
			case part.FunctionCall != nil:
				out = append(out, withUsage(PlatformEvent{
					Type: EventNodeToolCallStarted, Node: node,
					Payload: map[string]any{"name": part.FunctionCall.Name, "args": part.FunctionCall.Args},
				}))
			case part.FunctionResponse != nil:
				out = append(out, withUsage(PlatformEvent{
					Type: EventNodeToolCallFinished, Node: node,
					Payload: map[string]any{"name": part.FunctionResponse.Name, "result": part.FunctionResponse.Response},
				}))
			case part.Thought:
				// The model's own reasoning trace.
				out = append(out, withUsage(PlatformEvent{Type: EventNodeReasoning, Node: node, Payload: map[string]any{"text": part.Text}}))
			case part.Text != "":
				if ev.IsFinalResponse() {
					out = append(out, withUsage(PlatformEvent{Type: EventNodeFinished, Node: node, Payload: map[string]any{"text": part.Text}}))
				} else {
					// Streamed/intermediate model text is still the
					// model "thinking out loud" from the product's point
					// of view — spec-14 的打字机效果，但还不是这个节点已
					// 经定稿的输出。/chat/bundle/:ref 上的订阅者按定义永
					// 远不是 Bundle 作者，这条被挡掉时 spec-14 自己的验
					// 收项（"打字机效果，不是一次性整段出现"）对他们就是
					// 不可能达成的。
					out = append(out, withUsage(PlatformEvent{Type: EventNodeThinking, Node: node, Payload: map[string]any{"text": part.Text}}))
				}
			}
		}
	}

	if len(out) == 0 && ev.IsFinalResponse() {
		out = append(out, withUsage(PlatformEvent{Type: EventNodeFinished, Node: node, Payload: nil}))
	}
	return out
}
