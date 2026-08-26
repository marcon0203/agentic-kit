package modelgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── connectivity ─────────────────────────────────────────────────────

func TestAnthropicValidator_ValidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "good-key" {
			t.Fatalf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("anthropic", "", providerOverrides{"anthropic": srv.URL})
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestAnthropicValidator_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("anthropic", "", providerOverrides{"anthropic": srv.URL})
	err := v.Validate(context.Background(), "bad-key")
	if err != ErrCredentialsInvalid {
		t.Fatalf("expected ErrCredentialsInvalid, got %v", err)
	}
}

func TestOpenAIValidator_ValidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-key" {
			t.Fatalf("expected bearer header, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("openai", "", providerOverrides{"openai": srv.URL})
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestDeepSeekValidator_ValidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-key" {
			t.Fatalf("expected bearer header, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("deepseek", "", providerOverrides{"deepseek": srv.URL})
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestQwenValidator_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("qwen", "", providerOverrides{"qwen": srv.URL})
	if err := v.Validate(context.Background(), "bad-key"); err != ErrCredentialsInvalid {
		t.Fatalf("expected ErrCredentialsInvalid, got %v", err)
	}
}

func TestGoogleValidator_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("google", "", providerOverrides{"google": srv.URL})
	if err := v.Validate(context.Background(), "bad-key"); err != ErrCredentialsInvalid {
		t.Fatalf("expected ErrCredentialsInvalid, got %v", err)
	}
}

func TestValidator_Unreachable(t *testing.T) {
	v := newValidatorWithEndpoints("anthropic", "", providerOverrides{"anthropic": "http://127.0.0.1:1"})
	err := v.Validate(context.Background(), "any-key")
	if err == nil || err == ErrCredentialsInvalid {
		t.Fatalf("expected a network error distinct from ErrCredentialsInvalid, got %v", err)
	}
}

// "custom" has no documented endpoint of its own, so a Validator built for
// it without a base_url must fail rather than silently accept.
func TestNewValidator_CustomWithoutBaseURLFails(t *testing.T) {
	v := NewValidator("custom", "")
	if err := v.Validate(context.Background(), "anything"); err == nil {
		t.Fatal("expected custom provider without a base_url to fail validation")
	}
}

func TestNewValidator_CustomWithBaseURLValidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-key" {
			t.Fatalf("expected bearer header, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	v := NewValidator("custom", srv.URL)
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestNewValidator_UnknownProvider(t *testing.T) {
	if NewValidator("bogus", "") != nil {
		t.Fatalf("expected nil Validator for unknown provider")
	}
}

// ── ParseModelSpec ───────────────────────────────────────────────────

func TestParseModelSpec(t *testing.T) {
	spec, err := ParseModelSpec("anthropic/claude-sonnet-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Provider != "anthropic" || spec.Name != "claude-sonnet-5" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestParseModelSpec_Invalid(t *testing.T) {
	if _, err := ParseModelSpec("no-slash-here"); err == nil {
		t.Fatalf("expected an error for a spec without a slash")
	}
}

// ── completion clients ───────────────────────────────────────────────

func TestAnthropicClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "claude-sonnet-5" {
			t.Fatalf("unexpected model in request: %v", body["model"])
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hello there"}],"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "", "claude-sonnet-5", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hello there" || result.InputTokens != 10 || result.OutputTokens != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestAnthropicClient_SendsToolsAndParsesToolUse is the regression test for
// the bug this was written to fix: a model with capabilities.tools[]
// configured that behaved as if it had no tools at all, because
// CompletionRequest never carried a Tools field and no Client ever sent
// one. This asserts both directions: the outgoing request actually
// contains the tool's input_schema, the system prompt is a top-level
// field (not a "system"-role message, which the real Anthropic API
// rejects), and a tool_use response block round-trips into a ToolCall.
func TestAnthropicClient_SendsToolsAndParsesToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["system"] != "be helpful" {
			t.Fatalf("expected system prompt as a top-level field, got %v", body)
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool in request, got %v", body["tools"])
		}
		tool := tools[0].(map[string]any)
		if tool["name"] != "run_query" {
			t.Fatalf("unexpected tool name: %v", tool["name"])
		}
		schema := tool["input_schema"].(map[string]any)
		if schema["type"] != "object" {
			t.Fatalf("expected input_schema to carry a lowercase JSON Schema type, got %v", schema)
		}
		messages, _ := body["messages"].([]any)
		for _, m := range messages {
			if m.(map[string]any)["role"] == "system" {
				t.Fatal("system prompt must never appear as a message role for Anthropic")
			}
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","id":"toolu_1","name":"run_query","input":{"sql":"select 1"}}],"usage":{"input_tokens":20,"output_tokens":10}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "", "claude-sonnet-5", CompletionRequest{
		Messages: []Message{{Role: "system", Content: "be helpful"}, {Role: "user", Content: "how many agents?"}},
		Tools: []Tool{{
			Name: "run_query", Description: "runs a SQL query",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"sql": map[string]any{"type": "string"}}},
		}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "run_query" || result.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("unexpected tool calls: %+v", result.ToolCalls)
	}
	if result.ToolCalls[0].Arguments["sql"] != "select 1" {
		t.Fatalf("unexpected tool call arguments: %+v", result.ToolCalls[0].Arguments)
	}
}

// TestAnthropicClient_SendsToolResultForToolMessage verifies the reverse
// leg: replaying a tool's result back as the next turn produces a
// tool_result content block tagged with the same tool_use_id, not a plain
// user-role text message (which Anthropic would silently misinterpret as
// unrelated conversation, not a function result).
func TestAnthropicClient_SendsToolResultForToolMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		messages, _ := body["messages"].([]any)
		if len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %v", messages)
		}
		toolMsg := messages[1].(map[string]any)
		if toolMsg["role"] != "user" {
			t.Fatalf("tool results must be sent as role=user for Anthropic, got %v", toolMsg["role"])
		}
		blocks := toolMsg["content"].([]any)
		block := blocks[0].(map[string]any)
		if block["type"] != "tool_result" || block["tool_use_id"] != "toolu_1" || block["content"] != "3 agents" {
			t.Fatalf("unexpected tool_result block: %+v", block)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"there are 3 agents"}],"usage":{"input_tokens":5,"output_tokens":5}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{client: srv.Client(), baseURL: srv.URL}
	_, err := c.Complete(context.Background(), "key", "", "claude-sonnet-5", CompletionRequest{
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "toolu_1", Name: "run_query", Arguments: map[string]any{"sql": "select count(*) from agents"}}}},
			{Role: "tool", ToolCallID: "toolu_1", ToolName: "run_query", Content: "3 agents"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicClient_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{client: srv.Client(), baseURL: srv.URL}
	_, err := c.Complete(context.Background(), "key", "", "claude-sonnet-5", CompletionRequest{})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected rate limited error, got %v", err)
	}
}

func TestOpenAICompatibleClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("expected bearer header, got %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hi from openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`))
	}))
	defer srv.Close()

	c := &openAICompatibleClient{httpClient: srv.Client(), defaultBaseURL: srv.URL, label: "openai"}
	result, err := c.Complete(context.Background(), "key", "", "gpt-4o", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hi from openai" || result.InputTokens != 8 || result.OutputTokens != 4 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenAICompatibleClient_SendsToolsAndParsesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool in request, got %v", body["tools"])
		}
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		if fn["name"] != "run_query" {
			t.Fatalf("unexpected function name: %v", fn["name"])
		}
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"run_query","arguments":"{\"sql\":\"select 1\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`))
	}))
	defer srv.Close()

	c := &openAICompatibleClient{httpClient: srv.Client(), defaultBaseURL: srv.URL, label: "openai"}
	result, err := c.Complete(context.Background(), "key", "", "gpt-4o", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "how many agents?"}},
		Tools: []Tool{{
			Name: "run_query", Description: "runs a SQL query",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"sql": map[string]any{"type": "string"}}},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "run_query" || result.ToolCalls[0].ID != "call_1" {
		t.Fatalf("unexpected tool calls: %+v", result.ToolCalls)
	}
	if result.ToolCalls[0].Arguments["sql"] != "select 1" {
		t.Fatalf("unexpected tool call arguments: %+v", result.ToolCalls[0].Arguments)
	}
}

func TestOpenAICompatibleClient_SendsToolResultWithToolCallID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		messages, _ := body["messages"].([]any)
		toolMsg := messages[len(messages)-1].(map[string]any)
		if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "3 agents" {
			t.Fatalf("unexpected tool-result message: %+v", toolMsg)
		}
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"there are 3"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c := &openAICompatibleClient{httpClient: srv.Client(), defaultBaseURL: srv.URL, label: "openai"}
	_, err := c.Complete(context.Background(), "key", "", "gpt-4o", CompletionRequest{
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "run_query", Arguments: map[string]any{"sql": "select count(*) from agents"}}}},
			{Role: "tool", ToolCallID: "call_1", ToolName: "run_query", Content: "3 agents"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAICompatibleClient_Embed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Response order deliberately reversed to prove Embed sorts back
		// into request order by index rather than trusting response order.
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.4,0.5],"index":1},{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"text-embedding-3-small","usage":{"prompt_tokens":4,"total_tokens":4}}`))
	}))
	defer srv.Close()

	c := &openAICompatibleClient{httpClient: srv.Client(), defaultBaseURL: srv.URL, label: "openai"}
	vecs, err := c.Embed(context.Background(), "key", "", "text-embedding-3-small", []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][0] != 0.4 {
		t.Fatalf("unexpected vectors, not sorted into request order: %+v", vecs)
	}
}

func TestGateway_Embed_UnsupportedProviderReturnsClearError(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &anthropicClient{},
	}, nil)
	_, err := gw.Embed(context.Background(), ModelSpec{Provider: "anthropic", Name: "n/a"},
		map[string]Credential{"anthropic": {APIKey: "key"}}, []string{"hi"})
	if !errors.Is(err, ErrEmbeddingsNotSupported) {
		t.Fatalf("expected ErrEmbeddingsNotSupported, got %v", err)
	}
}

// DeepSeek and Qwen speak the identical OpenAI-compatible wire format, so
// the same client type serves them — this test exercises it under the
// deepseek label with a per-call baseURL override, the same path a
// "custom" Credential takes.
func TestOpenAICompatibleClient_DeepSeekViaBaseURLOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"hi from deepseek"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer srv.Close()

	c := &openAICompatibleClient{httpClient: srv.Client(), defaultBaseURL: "", label: "deepseek"}
	result, err := c.Complete(context.Background(), "key", srv.URL, "deepseek-chat", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hi from deepseek" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenAICompatibleClient_NoBaseURLConfigured(t *testing.T) {
	c := &openAICompatibleClient{httpClient: http.DefaultClient, defaultBaseURL: "", label: "custom"}
	_, err := c.Complete(context.Background(), "key", "", "some-model", CompletionRequest{})
	if err == nil || !strings.Contains(err.Error(), "no base_url configured") {
		t.Fatalf("expected a clear no-base_url error, got %v", err)
	}
}

func TestGoogleClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi from gemini"}]}}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":3}}`))
	}))
	defer srv.Close()

	c := &googleClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "", "gemini-1.5-pro", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hi from gemini" || result.InputTokens != 6 || result.OutputTokens != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoogleClient_SendsFunctionDeclarationsAndParsesFunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sysInstr, ok := body["systemInstruction"].(map[string]any)
		if !ok || sysInstr["parts"].([]any)[0].(map[string]any)["text"] != "be helpful" {
			t.Fatalf("expected systemInstruction as a top-level field, got %v", body)
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tools entry, got %v", body["tools"])
		}
		decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
		if len(decls) != 1 || decls[0].(map[string]any)["name"] != "run_query" {
			t.Fatalf("unexpected functionDeclarations: %v", decls)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"run_query","args":{"sql":"select 1"}}}]}}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":3}}`))
	}))
	defer srv.Close()

	c := &googleClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "", "gemini-1.5-pro", CompletionRequest{
		Messages: []Message{{Role: "system", Content: "be helpful"}, {Role: "user", Content: "how many agents?"}},
		Tools: []Tool{{
			Name: "run_query", Description: "runs a SQL query",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"sql": map[string]any{"type": "string"}}},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "run_query" {
		t.Fatalf("unexpected tool calls: %+v", result.ToolCalls)
	}
	if result.ToolCalls[0].Arguments["sql"] != "select 1" {
		t.Fatalf("unexpected tool call arguments: %+v", result.ToolCalls[0].Arguments)
	}
}

// ── fallback chain ───────────────────────────────────────────────────

type fakeClient struct {
	fail    bool
	content string
	in, out int64
}

func (c *fakeClient) Complete(context.Context, string, string, string, CompletionRequest) (CompletionResult, error) {
	if c.fail {
		return CompletionResult{}, &validationError{"simulated failure"}
	}
	return CompletionResult{Content: c.content, InputTokens: c.in, OutputTokens: c.out}, nil
}

type fakeSink struct {
	events []FallbackEvent
}

func (s *fakeSink) EmitFallback(_ context.Context, ev FallbackEvent) {
	s.events = append(s.events, ev)
}

func TestGateway_Complete_PrimarySucceeds_NoFallback(t *testing.T) {
	sink := &fakeSink{}
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &fakeClient{content: "primary answer", in: 100, out: 50},
	}, sink)

	result, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"}, nil,
		map[string]Credential{"anthropic": {APIKey: "key"}}, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "primary answer" || result.Provider != "anthropic" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.CostUSD != EstimateCost("anthropic", "claude-sonnet-5", 100, 50) {
		t.Fatalf("expected cost to be computed, got %v", result.CostUSD)
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected no fallback events when primary succeeds, got %+v", sink.events)
	}
}

func TestGateway_Complete_FallsBackOnPrimaryFailure(t *testing.T) {
	sink := &fakeSink{}
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &fakeClient{fail: true},
		"openai":    &fakeClient{content: "fallback answer", in: 20, out: 10},
	}, sink)

	result, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"},
		[]ModelSpec{{Provider: "openai", Name: "gpt-4o"}},
		map[string]Credential{"anthropic": {APIKey: "key1"}, "openai": {APIKey: "key2"}}, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "fallback answer" || result.Provider != "openai" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected exactly one fallback event, got %+v", sink.events)
	}
	ev := sink.events[0]
	if ev.FromProvider != "anthropic" || ev.ToProvider != "openai" {
		t.Fatalf("unexpected fallback event: %+v", ev)
	}
}

func TestGateway_Complete_AllFail_Returns60003Sentinel(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &fakeClient{fail: true},
		"openai":    &fakeClient{fail: true},
	}, nil)

	_, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"},
		[]ModelSpec{{Provider: "openai", Name: "gpt-4o"}},
		map[string]Credential{"anthropic": {APIKey: "key1"}, "openai": {APIKey: "key2"}}, CompletionRequest{})
	if err == nil {
		t.Fatal("expected an error when every provider fails")
	}
	if !isAllProvidersUnavailable(err) {
		t.Fatalf("expected error wrapping ErrAllProvidersUnavailable, got %v", err)
	}
}

func isAllProvidersUnavailable(err error) bool {
	for err != nil {
		if err == ErrAllProvidersUnavailable {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return strings.Contains(err.Error(), "all providers")
		}
		err = u.Unwrap()
	}
	return false
}

func TestGateway_Complete_MissingCredentials_TreatedAsFailure(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &fakeClient{content: "should not be reached"},
	}, nil)

	_, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"}, nil,
		map[string]Credential{}, CompletionRequest{})
	if err == nil {
		t.Fatal("expected an error when no credentials are configured for the provider")
	}
}

// The Gateway forwards a Credential's BaseURL through to the Client's per-
// call parameter — this is what lets a "custom" or overridden endpoint
// reach the wire without being baked into the Client at construction.
func TestGateway_Complete_ForwardsCredentialBaseURL(t *testing.T) {
	var gotBaseURL string
	gw := NewGatewayWithClients(map[string]Client{
		"custom": Client(baseURLCapturingClient(func(_ context.Context, _, baseURL, _ string, _ CompletionRequest) (CompletionResult, error) {
			gotBaseURL = baseURL
			return CompletionResult{Content: "ok"}, nil
		})),
	}, nil)

	_, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "custom", Name: "some-model"}, nil,
		map[string]Credential{"custom": {APIKey: "key", BaseURL: "https://my-proxy.example.com/v1"}}, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBaseURL != "https://my-proxy.example.com/v1" {
		t.Fatalf("expected the credential's base_url to reach the client, got %q", gotBaseURL)
	}
}

type baseURLCapturingClient func(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error)

func (f baseURLCapturingClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	return f(ctx, apiKey, baseURL, model, req)
}

// ── pricing ──────────────────────────────────────────────────────────

func TestEstimateCost_KnownModel(t *testing.T) {
	cost := EstimateCost("anthropic", "claude-sonnet-5", 1000, 1000)
	want := 0.003 + 0.015
	if cost != want {
		t.Fatalf("expected %v, got %v", want, cost)
	}
}

func TestEstimateCost_UnknownModel_ReturnsZero(t *testing.T) {
	if cost := EstimateCost("anthropic", "does-not-exist", 1000, 1000); cost != 0 {
		t.Fatalf("expected 0 for unknown model, got %v", cost)
	}
}

// ── registry ─────────────────────────────────────────────────────────

// Locks in the point of the registry: NewGateway, NewValidator and
// EstimateCost must all recognize every provider ProviderNames() reports,
// with no per-provider case needed anywhere outside registry.go.
func TestRegistry_EveryProviderIsWiredEverywhere(t *testing.T) {
	gw := NewGateway(nil)
	for _, name := range ProviderNames() {
		if _, ok := gw.clients[name]; !ok {
			t.Errorf("provider %q registered in ProviderNames() but missing from Gateway.clients", name)
		}
		if name == "custom" {
			// "custom" has no default endpoint — NewValidator("custom", "")
			// legitimately returns a Validator whose Validate always fails,
			// not a nil Validator, so it's covered by
			// TestNewValidator_CustomWithoutBaseURLFails above instead.
			continue
		}
		if NewValidator(name, "") == nil {
			t.Errorf("provider %q registered in ProviderNames() but NewValidator returned nil", name)
		}
	}
}

func TestProviderNames_IncludesEveryBuiltInProvider(t *testing.T) {
	want := []string{"anthropic", "openai", "google", "deepseek", "qwen", "custom"}
	got := ProviderNames()
	if len(got) != len(want) {
		t.Fatalf("expected %d providers, got %d: %v", len(want), len(got), got)
	}
	for _, name := range want {
		found := false
		for _, g := range got {
			if g == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in ProviderNames(), got %v", name, got)
		}
	}
}
