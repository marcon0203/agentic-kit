package modelgateway

import (
	"context"
	"errors"
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

// Locks in the point of the registry: a Gateway and NewValidator must both
// recognize every channel ProviderNames() reports, with no per-channel case
// anywhere outside registry.go.
func TestRegistry_EveryChannelIsWiredEverywhere(t *testing.T) {
	gw := NewGateway(nil)
	for _, name := range ProviderNames() {
		if _, ok := gw.clientFor(name); !ok {
			t.Errorf("渠道 %q 在 ProviderNames() 里但 Gateway 取不到 client", name)
		}
		if NewValidator(name, "") == nil {
			t.Errorf("渠道 %q 在 ProviderNames() 里但 NewValidator 返回 nil", name)
		}
	}
}

// 注册表是运行时可变的：管理员删掉一个渠道之后，用它的 Agent 必须报"没有
// 这个 client"，而不是继续用一个已经不该存在的缓存。
func TestSetChannels_ReplacesTheWholeRegistry(t *testing.T) {
	before := ProviderNames()
	t.Cleanup(func() { restoreTestChannels(t) })

	SetChannels(nil)
	if len(ProviderNames()) != 0 {
		t.Fatalf("清空后不该还有渠道: %v", ProviderNames())
	}
	if _, ok := NewGateway(nil).clientFor("deepseek"); ok {
		t.Error("渠道删掉之后 Gateway 不该还能取到它的 client")
	}
	if NewValidator("deepseek", "") != nil {
		t.Error("渠道删掉之后 NewValidator 应返回 nil")
	}

	restoreTestChannels(t)
	if len(ProviderNames()) != len(before) {
		t.Fatalf("恢复后渠道数不对: %v", ProviderNames())
	}
}

// ── 流式 + 降级链 ─────────────────────────────────────────────────────

// stubStreamClient 按脚本吐几段文字再（可选地）失败。
type stubStreamClient struct {
	deltas []string
	err    error
}

func (c *stubStreamClient) Complete(context.Context, string, string, string, CompletionRequest) (CompletionResult, error) {
	if c.err != nil {
		return CompletionResult{}, c.err
	}
	return CompletionResult{Content: strings.Join(c.deltas, "")}, nil
}

func (c *stubStreamClient) CompleteStream(_ context.Context, _, _, _ string, _ CompletionRequest, onDelta func(StreamDelta)) (CompletionResult, error) {
	for _, d := range c.deltas {
		onDelta(StreamDelta{TextDelta: d})
	}
	if c.err != nil {
		return CompletionResult{}, c.err
	}
	return CompletionResult{Content: strings.Join(c.deltas, "")}, nil
}

// clientSpy 记录"这一环有没有被调用过"。
type clientSpy struct {
	inner  Client
	onCall func()
}

func (c *clientSpy) Complete(ctx context.Context, k, b, m string, req CompletionRequest) (CompletionResult, error) {
	c.onCall()
	return c.inner.Complete(ctx, k, b, m, req)
}

func (c *clientSpy) CompleteStream(ctx context.Context, k, b, m string, req CompletionRequest, onDelta func(StreamDelta)) (CompletionResult, error) {
	c.onCall()
	return c.inner.(StreamingClient).CompleteStream(ctx, k, b, m, req, onDelta)
}

// 第一环还没吐出任何 delta 就失败（连不上、鉴权失败、模型名不存在）——调
// 用方什么都没看到，换一环是纯赚，降级照常。
func TestCompleteStream_FallsBackWhenNothingStreamedYet(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"deepseek":   &stubStreamClient{err: errors.New("401 unauthorized")},
		"volcengine": &stubStreamClient{deltas: []string{"你", "好"}},
	}, nil)

	var got []string
	result, err := gw.CompleteStream(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "deepseek-chat"},
		[]ModelSpec{{Provider: "volcengine", Name: "doubao"}},
		map[string]Credential{"deepseek": {APIKey: "k"}, "volcengine": {APIKey: "k"}},
		CompletionRequest{}, func(d StreamDelta) { got = append(got, d.TextDelta) })
	if err != nil {
		t.Fatalf("还没吐字就失败时应当降级: %v", err)
	}
	if result.Provider != "volcengine" || result.Content != "你好" {
		t.Fatalf("应由第二环作答: %+v", result)
	}
	if len(got) != 2 {
		t.Fatalf("期望两个增量，得到 %v", got)
	}
}

// ★ 这条盯住修掉的那个 bug：第一环已经把半段文字推给了前端才失败。降级会
// 让用户先看到 A 模型的半句话、再看到 B 模型的一整段，拼成读不通的东西，
// 而且没有任何提示说中间换过模型。半截答案比明确的失败更糟。
func TestCompleteStream_DoesNotFallBackAfterStreamingStarted(t *testing.T) {
	secondCalled := false
	gw := NewGatewayWithClients(map[string]Client{
		"deepseek": &stubStreamClient{deltas: []string{"我来查"}, err: errors.New("connection reset")},
		"volcengine": &clientSpy{
			inner:  &stubStreamClient{deltas: []string{"这段不该出现"}},
			onCall: func() { secondCalled = true },
		},
	}, nil)

	var got []string
	_, err := gw.CompleteStream(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "deepseek-chat"},
		[]ModelSpec{{Provider: "volcengine", Name: "doubao"}},
		map[string]Credential{"deepseek": {APIKey: "k"}, "volcengine": {APIKey: "k"}},
		CompletionRequest{}, func(d StreamDelta) { got = append(got, d.TextDelta) })

	if err == nil {
		t.Fatal("已经吐过字之后失败，必须直接报错而不是降级")
	}
	if !errors.Is(err, ErrStreamAlreadyStarted) {
		t.Errorf("错误应能被识别为「流已开始」，得到: %v", err)
	}
	// 上游的原始失败原因要带出来，否则只剩一句"没降级"没法排查。
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("错误里应带上上游的失败原因，得到: %v", err)
	}
	if secondCalled {
		t.Error("第二环根本不该被调用")
	}
	if len(got) != 1 || got[0] != "我来查" {
		t.Errorf("已经推出去的增量不回滚，但也不该有更多: %v", got)
	}
}

// 非流式路径不受影响：Complete 的降级链语义一点没变。
func TestComplete_StillFallsBackAfterAnyFailure(t *testing.T) {
	gw := NewGatewayWithClients(map[string]Client{
		"deepseek":   &stubStreamClient{err: errors.New("boom")},
		"volcengine": &stubStreamClient{deltas: []string{"ok"}},
	}, nil)
	result, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "a"},
		[]ModelSpec{{Provider: "volcengine", Name: "b"}},
		map[string]Credential{"deepseek": {APIKey: "k"}, "volcengine": {APIKey: "k"}},
		CompletionRequest{})
	if err != nil || result.Provider != "volcengine" {
		t.Fatalf("非流式降级不该受影响: %v / %+v", err, result)
	}
}
