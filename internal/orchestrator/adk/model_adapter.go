// Package adk is the only place in this codebase that imports
// google.golang.org/adk (spec-10: "所有 ADK 调用收敛在 internal/orchestrator/adk
// 包内，不散落到业务代码"). It compiles the platform's Agent/Bundle DSL into
// ADK Go 2.0 constructs and executes them — the platform keeps ownership of
// DSL validation, resource authorization, the event contract, and the
// black-box boundary; ADK owns per-agent LLM/tool-calling execution and
// session state storage.
package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

// gatewayLLM adapts internal/modelgateway.Gateway (spec-09's unified
// completion abstraction, already built and tested in Task 9) to ADK's
// model.LLM interface, so llmagent calls flow through the same
// fallback-chain/cost-tracking machinery instead of a second, ADK-native
// model integration.
type gatewayLLM struct {
	gateway   *modelgateway.Gateway
	primary   modelgateway.ModelSpec
	fallbacks []modelgateway.ModelSpec
	creds     map[string]modelgateway.Credential
}

// NewGatewayLLM builds a model.LLM for one Agent's model.provider +
// model.fallback chain (schemas/agent.schema.json), backed by gw.
func NewGatewayLLM(gw *modelgateway.Gateway, primary modelgateway.ModelSpec, fallbacks []modelgateway.ModelSpec, creds map[string]modelgateway.Credential) model.LLM {
	return &gatewayLLM{gateway: gw, primary: primary, fallbacks: fallbacks, creds: creds}
}

func (m *gatewayLLM) Name() string { return m.primary.Provider + "/" + m.primary.Name }

// GenerateContent implements model.LLM. stream is true whenever runner.go's
// RunConfig.StreamingMode is StreamingModeSSE (ADK's base flow decides this
// upstream, this adapter only reacts to it): each provider text delta then
// becomes its own Partial LLMResponse via modelgateway.Gateway.CompleteStream,
// followed by one final non-partial response carrying the aggregated
// result — exactly what ADK's iterator contract expects, and what
// TranslateEvent's IsFinalResponse() check already relies on to tell a
// node.thinking chunk from the node's committed node.finished output.
// stream=false (sub-agent calls that don't need live display, or a
// StreamingMode left at its zero value) takes the simpler single-shot
// Complete path, unchanged from before this existed.
func (m *gatewayLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages := contentsToMessages(req.Contents)
		if req.Config != nil && req.Config.SystemInstruction != nil {
			if sys := contentText(req.Config.SystemInstruction); sys != "" {
				messages = append([]modelgateway.Message{{Role: "system", Content: sys}}, messages...)
			}
		}
		tools := declaredTools(req)

		maxTokens := 0
		temperature := 0.0
		if req.Config != nil {
			if req.Config.MaxOutputTokens != 0 {
				maxTokens = int(req.Config.MaxOutputTokens)
			}
			if req.Config.Temperature != nil {
				temperature = float64(*req.Config.Temperature)
			}
		}

		// Visibility into the exact call each provider round-trip makes —
		// this is what "试运行看不到日志" needs: the message count/roles
		// and the tool names actually offered, right before the HTTP call
		// leaves the process.
		toolNames := make([]string, 0, len(tools))
		for _, t := range tools {
			toolNames = append(toolNames, t.Name)
		}
		slog.Info("model_generate_content", "provider", m.primary.Provider, "model", m.primary.Name,
			"message_count", len(messages), "tools", toolNames, "stream", stream)

		gwReq := modelgateway.CompletionRequest{Messages: messages, Tools: tools, MaxTokens: maxTokens, Temperature: temperature}

		var result modelgateway.CompletionResult
		var err error
		stopped := false
		if stream {
			// runner.go requests StreamingModeSSE, which is what makes ADK
			// pass stream=true here — each text delta becomes its own
			// Partial LLMResponse so the frontend's node.thinking
			// typewriter effect (timeline.ts, already built to accumulate
			// chunks) has something incremental to accumulate instead of
			// one response arriving all at once at the very end.
			result, err = m.gateway.CompleteStream(ctx, m.primary, m.fallbacks, m.creds, gwReq, func(d modelgateway.StreamDelta) {
				if stopped {
					return
				}
				// ReasoningDelta 单独标 Thought:true，走events.go 的
				// part.Thought 分支变成 node.reasoning；TextDelta 保持
				// 原样，走非 Thought 分支——events.go 据 IsFinalResponse
				// 判断它是不是 node.thinking 的打字机效果。
				if d.ReasoningDelta != "" {
					if !yield(&model.LLMResponse{
						Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: d.ReasoningDelta, Thought: true}}},
						Partial:      true,
						TurnComplete: false,
					}, nil) {
						stopped = true
						return
					}
				}
				if d.TextDelta != "" {
					if !yield(&model.LLMResponse{
						Content:      genai.NewContentFromText(d.TextDelta, genai.RoleModel),
						Partial:      true,
						TurnComplete: false,
					}, nil) {
						stopped = true
					}
				}
			})
		} else {
			result, err = m.gateway.Complete(ctx, m.primary, m.fallbacks, m.creds, gwReq)
		}
		if err != nil {
			slog.Warn("model_generate_content_failed", "provider", m.primary.Provider, "model", m.primary.Name, "error", err.Error())
			yield(nil, err)
			return
		}
		if stopped {
			return
		}
		slog.Info("model_generate_content_result", "provider", result.Provider, "model", result.Model,
			"content_chars", len(result.Content), "tool_calls", toolCallNames(result.ToolCalls))

		// stream=true 已经把 Reasoning 拆成增量 Thought part 逐条 yield
		// 过了（result.Reasoning 只是那些增量的聚合，重放一遍会把同一段
		// 思维链在前端重复展示一次）；只有 stream=false 这条从没推过任何
		// 中间 part 的路径，才需要在这里把它补上。
		if !stream && result.Reasoning != "" {
			yield(&model.LLMResponse{
				Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: result.Reasoning, Thought: true}}},
				Partial:      true,
				TurnComplete: false,
			}, nil)
		}

		yield(&model.LLMResponse{
			Content: resultToContent(result),
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(result.InputTokens),
				CandidatesTokenCount: int32(result.OutputTokens),
			},
			// CostUSD travels via CustomMetadata rather than a genai field
			// (genai has none) — TranslateEvent reads it back out so the
			// run engine's usage accounting (spec-09/11: accumulate onto
			// bundle_runs.total_tokens/cost_usd) doesn't need its own
			// second call into modelgateway's pricing table.
			CustomMetadata: map[string]any{"cost_usd": result.CostUSD},
			FinishReason:   genai.FinishReasonStop,
			TurnComplete:   true,
		}, nil)
	}
}

func toolCallNames(calls []modelgateway.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.Name)
	}
	return names
}

// declaredTools flattens every genai.Tool's FunctionDeclarations already
// packed into req.Config.Tools (ADK's tool/tool.go's PackTool does this
// packing at compile time for every ADK tool.Tool BuildPluginTool/
// BuildResourceTool et al. produce) into the platform's provider-agnostic
// Tool shape. req.Tools (map[string]any, the raw tool.Tool values) is not
// used here — Declaration()'s already-materialized genai.FunctionDeclaration
// is all a wire-format translation needs.
//
// A declaration's schema can arrive on either of two mutually-exclusive
// genai fields, and every tool this codebase builds via
// google.golang.org/adk/tool/functiontool (BuildPluginTool included) uses
// the second one, never the first: Parameters (*genai.Schema, the older
// Vertex-flavored shape) is what a hand-built declaration sets, but
// functiontool.New's own Declaration() always populates
// ParametersJsonSchema instead (a *jsonschema.Schema — see
// functiontool/function.go's `decl.ParametersJsonSchema = f.inputSchema.Schema()`).
// Reading only Parameters here would silently see an empty schema for
// every real tool in the platform, plugins included — this is what made
// even a fully-declared input_schema (schemas/plugin.schema.json's
// tools[].input_schema) never actually reach the model, forcing it to
// guess a call's shape and often needing several tries to get it right.
func declaredTools(req *model.LLMRequest) []modelgateway.Tool {
	if req.Config == nil {
		return nil
	}
	var out []modelgateway.Tool
	for _, t := range req.Config.Tools {
		if t == nil {
			continue
		}
		for _, decl := range t.FunctionDeclarations {
			if decl == nil {
				continue
			}
			out = append(out, modelgateway.Tool{
				Name: decl.Name, Description: decl.Description,
				InputSchema: declarationInputSchema(decl),
			})
		}
	}
	return out
}

// declarationInputSchema resolves whichever of the two schema fields a
// FunctionDeclaration actually populated into a standard JSON Schema
// object. ParametersJsonSchema is preferred — it's what every functiontool-
// built tool (i.e. nearly everything) uses, and it's already lowercase-
// typed standard JSON Schema once marshaled, so no case-mapping is needed
// the way genai.Schema's own upper-cased Type enum requires.
func declarationInputSchema(decl *genai.FunctionDeclaration) map[string]any {
	if decl.ParametersJsonSchema != nil {
		b, err := json.Marshal(decl.ParametersJsonSchema)
		if err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
	}
	return genaiSchemaToJSONSchema(decl.Parameters)
}

// genaiSchemaToJSONSchema converts genai's Vertex/Gemini-flavored Schema
// (upper-cased Type enum, "STRING"/"OBJECT"/…) into a standard JSON Schema
// object — what every provider's actual wire API (Anthropic's
// input_schema, OpenAI's function.parameters, and Gemini's own
// functionDeclarations.parameters, which despite the sibling Schema type
// documents itself as "OpenAPI 3.03 Parameter Object" and expects the same
// lowercase types) requires. nil in, nil out — a tool with no parameters
// (an empty pluginToolArgs-less function) is valid and shouldn't gain a
// synthetic empty object.
func genaiSchemaToJSONSchema(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = genaiTypeToJSONType(s.Type)
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if s.Items != nil {
		out["items"] = genaiSchemaToJSONSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			props[name] = genaiSchemaToJSONSchema(prop)
		}
		out["properties"] = props
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	return out
}

func genaiTypeToJSONType(t genai.Type) string {
	switch t {
	case genai.TypeString:
		return "string"
	case genai.TypeNumber:
		return "number"
	case genai.TypeInteger:
		return "integer"
	case genai.TypeBoolean:
		return "boolean"
	case genai.TypeArray:
		return "array"
	case genai.TypeObject:
		return "object"
	default:
		return "string"
	}
}

// contentsToMessages replays ADK's full turn history — including any
// FunctionCall/FunctionResponse parts a prior GenerateContent round
// produced — into the platform's provider-agnostic Message shape. Every
// tool-calling provider needs this replay to make sense of a "here's the
// result of the thing you asked me to call" turn; dropping it (as a
// text-only contentText walk does) is what made the model behave as if it
// had never been offered any tool at all.
func contentsToMessages(contents []*genai.Content) []modelgateway.Message {
	messages := make([]modelgateway.Message, 0, len(contents))
	// pendingCallIDs tracks, per function name, the IDs of unresolved
	// FunctionCalls in call order — genai.NewContentFromFunctionResponse
	// (what ADK's own flow uses to replay a tool's result) never carries
	// the call's ID, only its Name, so this is how the ID a provider
	// actually assigned gets recovered for the matching tool-result turn.
	// A plain map[string]string here is not enough: when a model calls the
	// same tool twice in one turn (e.g. two run_query calls), the second
	// call's ID would silently overwrite the first before either response
	// arrived, so one of the two original tool_call_ids would never get a
	// matching tool message — providers reject that outright ("assistant
	// message with tool_calls must be followed by tool messages responding
	// to each tool_call_id"). A FIFO queue per name resolves each response
	// to the earliest still-unanswered call with that name, which matches
	// how ADK's flow issues and resolves calls in order.
	pendingCallIDs := map[string][]string{}

	for _, c := range contents {
		if c == nil {
			continue
		}
		role := roleToMessageRole(c.Role)
		var text string
		var calls []modelgateway.ToolCall
		var responses []modelgateway.Message
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			switch {
			case p.FunctionCall != nil:
				id := p.FunctionCall.ID
				if id == "" {
					id = fmt.Sprintf("call_%s_%d", p.FunctionCall.Name, len(pendingCallIDs[p.FunctionCall.Name]))
				}
				pendingCallIDs[p.FunctionCall.Name] = append(pendingCallIDs[p.FunctionCall.Name], id)
				calls = append(calls, modelgateway.ToolCall{ID: id, Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args})
			case p.FunctionResponse != nil:
				var id string
				if queue := pendingCallIDs[p.FunctionResponse.Name]; len(queue) > 0 {
					id, pendingCallIDs[p.FunctionResponse.Name] = queue[0], queue[1:]
				} else {
					id = "call_" + p.FunctionResponse.Name
				}
				responses = append(responses, modelgateway.Message{
					Role: "tool", Content: functionResponseText(p.FunctionResponse),
					ToolCallID: id, ToolName: p.FunctionResponse.Name,
				})
			case p.Text != "":
				if text != "" {
					text += "\n"
				}
				text += p.Text
			}
		}
		if len(calls) > 0 {
			messages = append(messages, modelgateway.Message{Role: "assistant", Content: text, ToolCalls: calls})
			continue
		}
		if len(responses) > 0 {
			messages = append(messages, responses...)
			continue
		}
		if text != "" || role != "" {
			messages = append(messages, modelgateway.Message{Role: role, Content: text})
		}
	}
	return messages
}

// functionResponseText flattens a FunctionResponse's Response map into the
// plain-text tool-result content every provider's wire format wants — the
// tool's own output (pluginToolResult{Output: "..."}, see plugin_tools.go)
// lands under the "output" key.
func functionResponseText(fr *genai.FunctionResponse) string {
	if fr == nil || fr.Response == nil {
		return ""
	}
	if out, ok := fr.Response["output"].(string); ok {
		return out
	}
	return fmt.Sprint(fr.Response)
}

// resultToContent builds the genai.Content ADK's flow expects back: one
// FunctionCall part per tool the model decided to invoke (which the flow
// then actually runs and replays as a FunctionResponse on the next turn),
// or a plain text part when the model just answered.
func resultToContent(result modelgateway.CompletionResult) *genai.Content {
	if len(result.ToolCalls) == 0 {
		return genai.NewContentFromText(result.Content, genai.RoleModel)
	}
	parts := make([]*genai.Part, 0, len(result.ToolCalls)+1)
	if result.Content != "" {
		parts = append(parts, genai.NewPartFromText(result.Content))
	}
	for _, c := range result.ToolCalls {
		parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: c.ID, Name: c.Name, Args: c.Arguments}})
	}
	return &genai.Content{Role: string(genai.RoleModel), Parts: parts}
}

func roleToMessageRole(role string) string {
	switch role {
	case string(genai.RoleModel):
		return "assistant"
	case "system":
		return "system"
	default:
		return "user"
	}
}

func contentText(c *genai.Content) string {
	var text string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += p.Text
		}
	}
	return text
}

// ErrNoAPIKey is returned by CompileAgent when the Agent DSL names a
// provider the caller supplied no credential for.
var ErrNoAPIKey = fmt.Errorf("adk: no API key configured for the agent's model provider")
