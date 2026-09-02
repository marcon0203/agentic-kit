package adk

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

type fakeGWClient struct {
	content string
	in, out int64
}

func (c *fakeGWClient) Complete(context.Context, string, string, string, modelgateway.CompletionRequest) (modelgateway.CompletionResult, error) {
	return modelgateway.CompletionResult{Content: c.content, InputTokens: c.in, OutputTokens: c.out}, nil
}

func TestGatewayLLM_GenerateContent(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{
		"deepseek": &fakeGWClient{content: "hello from the model", in: 12, out: 6},
	}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	if llm.Name() != "deepseek/deepseek-chat" {
		t.Fatalf("unexpected Name(): %q", llm.Name())
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
	}
	var got *model.LLMResponse
	for resp, err := range llm.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = resp
	}
	if got == nil {
		t.Fatal("expected a response")
	}
	if contentText(got.Content) != "hello from the model" {
		t.Fatalf("unexpected content: %q", contentText(got.Content))
	}
	if got.UsageMetadata.PromptTokenCount != 12 || got.UsageMetadata.CandidatesTokenCount != 6 {
		t.Fatalf("unexpected usage metadata: %+v", got.UsageMetadata)
	}
	if _, ok := got.CustomMetadata["cost_usd"]; !ok {
		t.Fatalf("expected CustomMetadata to carry cost_usd, got %+v", got.CustomMetadata)
	}
}

func TestGatewayLLM_GenerateContent_PropagatesError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil) // no client registered
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil, map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	var gotErr error
	for _, err := range llm.GenerateContent(context.Background(), req, false) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("expected an error when the gateway has no client for the provider")
	}
}

// fakeToolGWClient records the CompletionRequest it received and returns a
// canned tool-call result — used to verify GenerateContent actually hands
// declared tools through to modelgateway, the exact thing that was
// silently dropped before this fix.
type fakeToolGWClient struct {
	gotReq    modelgateway.CompletionRequest
	toolCalls []modelgateway.ToolCall
}

func (c *fakeToolGWClient) Complete(_ context.Context, _, _, _ string, req modelgateway.CompletionRequest) (modelgateway.CompletionResult, error) {
	c.gotReq = req
	return modelgateway.CompletionResult{ToolCalls: c.toolCalls, InputTokens: 1, OutputTokens: 1}, nil
}

func TestGatewayLLM_GenerateContent_PassesDeclaredToolsThrough(t *testing.T) {
	client := &fakeToolGWClient{toolCalls: []modelgateway.ToolCall{{ID: "call_1", Name: "run_query", Arguments: map[string]any{"sql": "select 1"}}}}
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{"deepseek": client}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("how many agents?", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "run_query", Description: "runs a SQL query",
				Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
					"sql": {Type: genai.TypeString},
				}},
			}}}},
		},
	}

	var got *model.LLMResponse
	for resp, err := range llm.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = resp
	}

	if len(client.gotReq.Tools) != 1 || client.gotReq.Tools[0].Name != "run_query" {
		t.Fatalf("expected the declared tool to reach modelgateway, got %+v", client.gotReq.Tools)
	}
	schema := client.gotReq.Tools[0].InputSchema
	if schema["type"] != "object" {
		t.Fatalf("expected a lowercase JSON Schema type, got %+v", schema)
	}
	if got.Content == nil || len(got.Content.Parts) != 1 || got.Content.Parts[0].FunctionCall == nil {
		t.Fatalf("expected the response Content to carry a FunctionCall part, got %+v", got.Content)
	}
	if got.Content.Parts[0].FunctionCall.Name != "run_query" || got.Content.Parts[0].FunctionCall.ID != "call_1" {
		t.Fatalf("unexpected FunctionCall: %+v", got.Content.Parts[0].FunctionCall)
	}
}

// TestGatewayLLM_GenerateContent_ReplaysFunctionCallHistory is the other
// half of the regression: a prior turn's FunctionCall/FunctionResponse
// parts (what ADK's flow replays as conversation history on the next
// GenerateContent call) must survive translation, not be silently dropped
// the way a text-only contentText walk would drop them.
func TestGatewayLLM_GenerateContent_ReplaysFunctionCallHistory(t *testing.T) {
	client := &fakeToolGWClient{}
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{"deepseek": client}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("how many agents?", genai.RoleUser),
			{Role: string(genai.RoleModel), Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "run_query", Args: map[string]any{"sql": "select count(*) from agents"}}}}},
			genai.NewContentFromFunctionResponse("run_query", map[string]any{"output": "3 agents"}, genai.RoleUser),
		},
	}
	for range llm.GenerateContent(context.Background(), req, false) {
	}

	if len(client.gotReq.Messages) != 3 {
		t.Fatalf("expected 3 replayed messages, got %+v", client.gotReq.Messages)
	}
	assistantMsg := client.gotReq.Messages[1]
	if assistantMsg.Role != "assistant" || len(assistantMsg.ToolCalls) != 1 || assistantMsg.ToolCalls[0].ID != "call_1" {
		t.Fatalf("unexpected assistant tool-call message: %+v", assistantMsg)
	}
	toolMsg := client.gotReq.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Content != "3 agents" {
		t.Fatalf("expected the function response to correlate back to call_1 via history, got %+v", toolMsg)
	}
}

// TestGatewayLLM_GenerateContent_ReplaysDuplicateToolCallsInOneTurn is the
// regression test for the follow-up bug: a model calling the same tool
// twice in one turn (e.g. run_query, run_query) previously lost the first
// call's ID — a plain map[string]string keyed by function name let the
// second FunctionCall silently overwrite the first before either
// FunctionResponse arrived, so one tool_call_id never got a matching tool
// message and OpenAI-compatible providers rejected the whole request
// ("assistant message with tool_calls must be followed by tool messages
// responding to each tool_call_id"). Both calls must now resolve to
// distinct IDs via FIFO order.
func TestGatewayLLM_GenerateContent_ReplaysDuplicateToolCallsInOneTurn(t *testing.T) {
	client := &fakeToolGWClient{}
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{"deepseek": client}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-v4-flash"}, nil,
		map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("list two tables", genai.RoleUser),
			{Role: string(genai.RoleModel), Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "run_query", Args: map[string]any{"sql": "select 1"}}},
				{FunctionCall: &genai.FunctionCall{ID: "call_2", Name: "run_query", Args: map[string]any{"sql": "select 2"}}},
			}},
			genai.NewContentFromFunctionResponse("run_query", map[string]any{"output": "result 1"}, genai.RoleUser),
			genai.NewContentFromFunctionResponse("run_query", map[string]any{"output": "result 2"}, genai.RoleUser),
		},
	}
	for range llm.GenerateContent(context.Background(), req, false) {
	}

	assistantMsg := client.gotReq.Messages[1]
	if len(assistantMsg.ToolCalls) != 2 || assistantMsg.ToolCalls[0].ID != "call_1" || assistantMsg.ToolCalls[1].ID != "call_2" {
		t.Fatalf("unexpected assistant tool calls: %+v", assistantMsg.ToolCalls)
	}
	toolMsg1, toolMsg2 := client.gotReq.Messages[2], client.gotReq.Messages[3]
	if toolMsg1.ToolCallID != "call_1" || toolMsg1.Content != "result 1" {
		t.Fatalf("expected the first response to resolve call_1, got %+v", toolMsg1)
	}
	if toolMsg2.ToolCallID != "call_2" || toolMsg2.Content != "result 2" {
		t.Fatalf("expected the second response to resolve call_2 (not overwritten by call_1), got %+v", toolMsg2)
	}
}

// fakeStreamingGWClient implements both modelgateway.Client and
// modelgateway.StreamingClient — used to verify GenerateContent actually
// takes the CompleteStream path (and yields Partial responses per delta)
// when stream=true, instead of the single-shot Complete path.
type fakeStreamingGWClient struct{ deltas []string }

func (c *fakeStreamingGWClient) Complete(context.Context, string, string, string, modelgateway.CompletionRequest) (modelgateway.CompletionResult, error) {
	panic("Complete should not be called when stream=true and the client supports CompleteStream")
}

func (c *fakeStreamingGWClient) CompleteStream(_ context.Context, _, _, _ string, _ modelgateway.CompletionRequest, onDelta func(modelgateway.StreamDelta)) (modelgateway.CompletionResult, error) {
	var full strings.Builder
	for _, d := range c.deltas {
		onDelta(modelgateway.StreamDelta{TextDelta: d})
		full.WriteString(d)
	}
	return modelgateway.CompletionResult{Content: full.String(), InputTokens: 1, OutputTokens: 1}, nil
}

// TestGatewayLLM_GenerateContent_StreamTrueYieldsPartialChunksThenFinal is
// the regression test for "试运行的时候消息没有流式输出": runner.go now
// requests StreamingModeSSE, which makes ADK pass stream=true here — this
// asserts GenerateContent actually takes the streaming path (not just
// silently falling back to one blocking call) and yields one Partial
// LLMResponse per delta before the final non-partial one.
func TestGatewayLLM_GenerateContent_StreamTrueYieldsPartialChunksThenFinal(t *testing.T) {
	client := &fakeStreamingGWClient{deltas: []string{"Hello", ", world"}}
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{"deepseek": client}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]modelgateway.Credential{"deepseek": {APIKey: "sk-test"}})

	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	var responses []*model.LLMResponse
	for resp, err := range llm.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, resp)
	}

	if len(responses) != 3 {
		t.Fatalf("expected 2 partial chunks + 1 final response, got %d: %+v", len(responses), responses)
	}
	if !responses[0].Partial || contentText(responses[0].Content) != "Hello" {
		t.Fatalf("unexpected first partial response: %+v", responses[0])
	}
	if !responses[1].Partial || contentText(responses[1].Content) != ", world" {
		t.Fatalf("unexpected second partial response: %+v", responses[1])
	}
	if responses[2].Partial || !responses[2].TurnComplete || contentText(responses[2].Content) != "Hello, world" {
		t.Fatalf("unexpected final response: %+v", responses[2])
	}
}

func TestRoleToMessageRole(t *testing.T) {
	cases := map[string]string{string(genai.RoleModel): "assistant", "system": "system", string(genai.RoleUser): "user", "": "user"}
	for in, want := range cases {
		if got := roleToMessageRole(in); got != want {
			t.Errorf("roleToMessageRole(%q) = %q, want %q", in, got, want)
		}
	}
}
