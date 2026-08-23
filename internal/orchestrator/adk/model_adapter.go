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
	apiKeys   map[string]string
}

// NewGatewayLLM builds a model.LLM for one Agent's model.provider +
// model.fallback chain (schemas/agent.schema.json), backed by gw.
func NewGatewayLLM(gw *modelgateway.Gateway, primary modelgateway.ModelSpec, fallbacks []modelgateway.ModelSpec, apiKeys map[string]string) model.LLM {
	return &gatewayLLM{gateway: gw, primary: primary, fallbacks: fallbacks, apiKeys: apiKeys}
}

func (m *gatewayLLM) Name() string { return m.primary.Provider + "/" + m.primary.Name }

// GenerateContent implements model.LLM. Streaming isn't supported by the
// modelgateway completion abstraction yet (spec-09 defines a request/
// response call, not token streaming) — a stream=true request still gets a
// single, complete LLMResponse, which is valid per ADK's iterator contract
// (a non-streaming response is just an iterator of length one).
func (m *gatewayLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages := make([]modelgateway.Message, 0, len(req.Contents))
		for _, c := range req.Contents {
			messages = append(messages, modelgateway.Message{Role: roleToMessageRole(c.Role), Content: contentText(c)})
		}

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

		result, err := m.gateway.Complete(ctx, m.primary, m.fallbacks, m.apiKeys, modelgateway.CompletionRequest{
			Messages: messages, MaxTokens: maxTokens, Temperature: temperature,
		})
		if err != nil {
			yield(nil, err)
			return
		}

		yield(&model.LLMResponse{
			Content: genai.NewContentFromText(result.Content, genai.RoleModel),
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(result.InputTokens),
				CandidatesTokenCount: int32(result.OutputTokens),
			},
			FinishReason: genai.FinishReasonStop,
			TurnComplete: true,
		}, nil)
	}
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
