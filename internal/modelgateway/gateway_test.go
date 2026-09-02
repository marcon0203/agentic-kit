package modelgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── connectivity ─────────────────────────────────────────────────────

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
	v := newValidatorWithEndpoints("deepseek", "", providerOverrides{"deepseek": "http://127.0.0.1:1"})
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
	spec, err := ParseModelSpec("deepseek/deepseek-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Provider != "deepseek" || spec.Name != "deepseek-chat" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestParseModelSpec_Invalid(t *testing.T) {
	if _, err := ParseModelSpec("no-slash-here"); err == nil {
		t.Fatalf("expected an error for a spec without a slash")
	}
}

// ── completion clients ───────────────────────────────────────────────

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

func TestGoogleClient_CompleteStream_StreamsTextDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Fatalf("expected the streaming endpoint, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":", world"}]}}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":3}}`,
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
		}
	}))
	defer srv.Close()

	c := &googleClient{client: srv.Client(), baseURL: srv.URL}
	var deltas []string
	result, err := c.CompleteStream(context.Background(), "key", "", "gemini-1.5-pro", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d StreamDelta) { deltas = append(deltas, d.TextDelta) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "Hello" || deltas[1] != ", world" {
		t.Fatalf("expected 2 incremental deltas, got %v", deltas)
	}
	if result.Content != "Hello, world" || result.InputTokens != 6 || result.OutputTokens != 3 {
		t.Fatalf("unexpected aggregated result: %+v", result)
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

// TestGateway_CompleteStream_DegradesGracefullyForNonStreamingClient
// verifies a Client that doesn't implement StreamingClient still works
// through CompleteStream — one onDelta call with the whole answer, rather
// than an error or a silently-empty stream.
func TestGateway_CompleteStream_DegradesGracefullyForNonStreamingClient(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"anthropic": &fakeClient{content: "the whole answer at once", in: 10, out: 5},
	}, nil)
	var deltas []string
	result, err := gw.CompleteStream(context.Background(), ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"}, nil,
		map[string]Credential{"anthropic": {APIKey: "key"}}, CompletionRequest{}, func(d StreamDelta) { deltas = append(deltas, d.TextDelta) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "the whole answer at once" {
		t.Fatalf("expected one delta carrying the whole answer, got %v", deltas)
	}
	if result.Content != "the whole answer at once" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// 价格表现在长在渠道描述符里（internal/builtinchannels/deepseek.json），
// EstimateCost 读的是同一张表——加渠道不用再改 Go 侧的第二份价格表。
func TestEstimateCost_KnownModel(t *testing.T) {
	cost := EstimateCost("deepseek", "deepseek-chat", 1000, 1000)
	want := 0.00027 + 0.0011
	// 浮点比较留个容差：价格是小数相加，精确相等在这里没有意义。
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected %v, got %v", want, cost)
	}
}

func TestEstimateCost_UnknownModel_ReturnsZero(t *testing.T) {
	if cost := EstimateCost("deepseek", "does-not-exist", 1000, 1000); cost != 0 {
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
