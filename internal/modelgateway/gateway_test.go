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

func TestAnthropicValidator_ValidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "good-key" {
			t.Fatalf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("anthropic", providerEndpoints{AnthropicBaseURL: srv.URL})
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestAnthropicValidator_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("anthropic", providerEndpoints{AnthropicBaseURL: srv.URL})
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

	v := newValidatorWithEndpoints("openai", providerEndpoints{OpenAIBaseURL: srv.URL})
	if err := v.Validate(context.Background(), "good-key"); err != nil {
		t.Fatalf("expected valid key to pass, got %v", err)
	}
}

func TestGoogleValidator_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	v := newValidatorWithEndpoints("google", providerEndpoints{GoogleBaseURL: srv.URL})
	if err := v.Validate(context.Background(), "bad-key"); err != ErrCredentialsInvalid {
		t.Fatalf("expected ErrCredentialsInvalid, got %v", err)
	}
}

func TestValidator_Unreachable(t *testing.T) {
	v := newValidatorWithEndpoints("anthropic", providerEndpoints{AnthropicBaseURL: "http://127.0.0.1:1"})
	err := v.Validate(context.Background(), "any-key")
	if err == nil || err == ErrCredentialsInvalid {
		t.Fatalf("expected a network error distinct from ErrCredentialsInvalid, got %v", err)
	}
}

func TestNewValidator_CustomAlwaysAccepts(t *testing.T) {
	v := NewValidator("custom")
	if err := v.Validate(context.Background(), "anything"); err != nil {
		t.Fatalf("custom provider should accept on trust, got %v", err)
	}
}

func TestNewValidator_UnknownProvider(t *testing.T) {
	if NewValidator("bogus") != nil {
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
	result, err := c.Complete(context.Background(), "key", "claude-sonnet-5", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hello there" || result.InputTokens != 10 || result.OutputTokens != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestAnthropicClient_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{client: srv.Client(), baseURL: srv.URL}
	_, err := c.Complete(context.Background(), "key", "claude-sonnet-5", CompletionRequest{})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected rate limited error, got %v", err)
	}
}

func TestOpenAIClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi from openai"}}],"usage":{"prompt_tokens":8,"completion_tokens":4}}`))
	}))
	defer srv.Close()

	c := &openaiClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "gpt-4o", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hi from openai" || result.InputTokens != 8 || result.OutputTokens != 4 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoogleClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi from gemini"}]}}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":3}}`))
	}))
	defer srv.Close()

	c := &googleClient{client: srv.Client(), baseURL: srv.URL}
	result, err := c.Complete(context.Background(), "key", "gemini-1.5-pro", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hi from gemini" || result.InputTokens != 6 || result.OutputTokens != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// ── fallback chain ───────────────────────────────────────────────────

type fakeClient struct {
	fail    bool
	content string
	in, out int64
}

func (c *fakeClient) Complete(context.Context, string, string, CompletionRequest) (CompletionResult, error) {
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
		map[string]string{"anthropic": "key"}, CompletionRequest{})
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
		map[string]string{"anthropic": "key1", "openai": "key2"}, CompletionRequest{})
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
		map[string]string{"anthropic": "key1", "openai": "key2"}, CompletionRequest{})
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
		map[string]string{}, CompletionRequest{})
	if err == nil {
		t.Fatal("expected an error when no credentials are configured for the provider")
	}
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
