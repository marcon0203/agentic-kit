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

// GenerateContent implements model.LLM. Streaming isn't supported by the
// modelgateway completion abstraction yet (spec-09 defines a request/
// response call, not token streaming) — a stream=true request still gets a
// single, complete LLMResponse, which is valid per ADK's iterator contract
// (a non-streaming response is just an iterator of length one).
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
			"message_count", len(messages), "tools", toolNames)

		result, err := m.gateway.Complete(ctx, m.primary, m.fallbacks, m.creds, modelgateway.CompletionRequest{
			Messages: messages, Tools: tools, MaxTokens: maxTokens, Temperature: temperature,
		})
		if err != nil {
			slog.Warn("model_generate_content_failed", "provider", m.primary.Provider, "model", m.primary.Name, "error", err.Error())
			yield(nil, err)
			return
		}
		slog.Info("model_generate_content_result", "provider", result.Provider, "model", result.Model,
			"content_chars", len(result.Content), "tool_calls", toolCallNames(result.ToolCalls))

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
				InputSchema: genaiSchemaToJSONSchema(decl.Parameters),
			})
		}
	}
	return out
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
	// pendingCallID tracks, by function name, the ID of the most recent
	// unresolved FunctionCall — genai.NewContentFromFunctionResponse (what
	// ADK's own flow uses to replay a tool's result) never carries the
	// call's ID, only its Name, so this is how the ID a provider actually
	// assigned gets recovered for the matching tool-result turn.
	pendingCallID := map[string]string{}

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
					id = "call_" + p.FunctionCall.Name
				}
				pendingCallID[p.FunctionCall.Name] = id
				calls = append(calls, modelgateway.ToolCall{ID: id, Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args})
			case p.FunctionResponse != nil:
				id, ok := pendingCallID[p.FunctionResponse.Name]
				if !ok {
					id = "call_" + p.FunctionResponse.Name
				}
				delete(pendingCallID, p.FunctionResponse.Name)
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
