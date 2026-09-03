package modelgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// completionTimeout bounds a single provider call. Fallback tries the next
// provider on any failure, including a timeout — a hung primary must not
// block the whole chain.
const completionTimeout = 60 * time.Second

// Message is one turn in a completion request, provider-agnostic.
//
// Role "tool" and the ToolCalls/ToolCallID/ToolName fields exist for
// function-calling: an "assistant" message with ToolCalls set is the model
// deciding to invoke tool(s) instead of (or alongside) answering in
// Content; a "tool" message with ToolCallID/ToolName set is that call's
// result being replayed back for the next turn.
type Message struct {
	Role    string // "user" | "assistant" | "system" | "tool"
	Content string
	// ToolCalls is set on an "assistant" message that invoked one or more
	// tools instead of, or alongside, answering directly.
	ToolCalls []ToolCall
	// ToolCallID/ToolName identify which call a "tool" role message is the
	// result of — ToolCallID must match the ID on the ToolCall it answers.
	ToolCallID string
	ToolName   string
}

// Tool is a provider-agnostic function-calling declaration: what
// capabilities.tools[] resolves into once compiled by the ADK layer, then
// translated once more here into each provider's own tool-definition wire
// shape (Anthropic's input_schema, the OpenAI-compatible family's
// parameters, Google's functionDeclarations).
type Tool struct {
	Name        string
	Description string
	// InputSchema is a standard JSON Schema object — lowercase "type"
	// values, "properties"/"required"/"items" etc. — never genai's own
	// upper-cased Schema shape, which no provider's wire API accepts as-is.
	InputSchema map[string]any
}

// ToolCall is one function invocation the model decided to make. ID is
// provider-assigned (Anthropic's tool_use.id, OpenAI's tool_calls[].id) and
// must be echoed back verbatim on the Message that carries this call's
// result, so the provider can correlate the two turns.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// CompletionRequest is the platform-side shape of an LLM call; each Client
// translates it into its provider's own wire format.
type CompletionRequest struct {
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature float64
	// Options 是渠道私有参数，经描述符的 $.options.<name> 进 body 模板。
	// 模型目录里按模型配置的参数取值（保留名 max_tokens/temperature 之外
	// 的）在 Gateway 层并进来；调用方也可以自带。
	Options map[string]any
}

// CompletionResult is what every Client normalizes its provider's response
// into. Provider/Model report which link in the fallback chain actually
// answered; CostUSD is computed by the Gateway after a successful call, not
// by the Client itself (pricing isn't provider-reported). ToolCalls is set
// instead of (or alongside) Content when the model decided to invoke one or
// more of the Tools it was offered.
type CompletionResult struct {
	Content      string
	ToolCalls    []ToolCall
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	Provider     string
	Model        string
}

// Credential is what a caller supplies to reach one provider: the API key,
// and — for an OpenAI-compatible provider whose endpoint isn't fixed
// (spec-09's 自定义 provider, and any built-in provider a caller wants to
// point at a proxy or regional mirror) — the base URL to send requests to.
// BaseURL empty means "use that provider's documented default"; for
// "custom" there is no default, so an empty BaseURL there is a
// configuration error the connectivity check catches at registration time.
type Credential struct {
	APIKey  string
	BaseURL string
	// Fields 是渠道描述符 credentials[] 声明的其它字段（region、
	// account_id 之类）。APIKey/BaseURL 是所有渠道都有的两个，单独留成
	// 具名字段；其余的形状由渠道自己声明，前端按声明渲染表单。
	//
	// 只有 type 非 secret 的字段会进到描述符的表达式作用域——密钥永远只
	// 经鉴权驱动进入请求。
	Fields map[string]string
}

// Client speaks one provider's completion API. baseURL is the resolved
// endpoint for this call — empty means the Client's own default applies;
// it is a parameter rather than baked into the Client at construction
// because a "custom" or overridden endpoint is a per-owner Credential, not
// a fact fixed by the provider name.
type Client interface {
	Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error)
}

// EmbeddingClient is implemented by Clients that also speak an embeddings
// API. Not every provider does (Anthropic has none; Google's is a
// different wire shape not yet implemented here), so this is a separate,
// optional interface rather than a method on Client — a provider that
// can't embed simply doesn't implement it, and Gateway.Embed reports that
// plainly instead of every Client needing a stub that always errors.
type EmbeddingClient interface {
	Embed(ctx context.Context, apiKey, baseURL, model string, texts []string) ([][]float32, error)
}

// ErrEmbeddingsNotSupported is returned by Gateway.Embed when the
// resolved provider's Client doesn't implement EmbeddingClient.
var ErrEmbeddingsNotSupported = errors.New("modelgateway: provider does not support embeddings")

// ModelSpec is a parsed "provider/name" DSL reference (spec-09: Agent DSL's
// model.provider + model.fallback[] are resolved into a real call chain
// here — never in the orchestrator).
type ModelSpec struct {
	Provider string
	Name     string
}

var fallbackSpecPattern = regexp.MustCompile(`^[a-z]+/.+$`)

// ParseModelSpec parses the "provider/name" format used by
// Agent.model.fallback[] (schemas/agent.schema.json).
func ParseModelSpec(s string) (ModelSpec, error) {
	if !fallbackSpecPattern.MatchString(s) {
		return ModelSpec{}, fmt.Errorf("modelgateway: invalid model spec %q, want provider/name", s)
	}
	parts := strings.SplitN(s, "/", 2)
	return ModelSpec{Provider: parts[0], Name: parts[1]}, nil
}

// FallbackEvent is emitted every time the Gateway moves to the next link in
// the chain, so a run's timeline can show "a downgrade happened" instead of
// silently switching models (spec-09).
type FallbackEvent struct {
	FromProvider string
	FromModel    string
	ToProvider   string
	ToModel      string
	Reason       string
}

// EventSink receives FallbackEvents. The orchestrator (task 10/11) is
// expected to implement this against the platform's run-event stream; it's
// declared here rather than depended on, so modelgateway stays free of any
// import on the orchestrator or the HTTP layer.
type EventSink interface {
	EmitFallback(ctx context.Context, ev FallbackEvent)
}

// ErrAllProvidersUnavailable is returned by Complete when every link in the
// fallback chain failed — the HTTP layer maps this to error code 60003.
var ErrAllProvidersUnavailable = errors.New("modelgateway: all providers in the fallback chain are unavailable")

// Gateway resolves an Agent's model.provider + model.fallback chain into
// real calls, in order, stopping at the first success.
//
// 它不再在构造时把 client 缓存成一张 map：渠道现在是管理员在运行时增删的
// 配置，缓存下来会让"刚建好的渠道要重启才能用"。descriptorClient 是个很轻
// 的结构体，每次调用现建一个的代价可以忽略。
type Gateway struct {
	sink       EventSink
	httpClient *http.Client
	// overrides 非 nil 时完全取代注册表——只有测试用（NewGatewayWithClients）。
	overrides map[string]Client
}

// NewGateway builds a Gateway that resolves channels from the live
// registry (registry.go). sink may be nil (no fallback events emitted).
func NewGateway(sink EventSink) *Gateway {
	return &Gateway{sink: sink, httpClient: &http.Client{Timeout: completionTimeout}}
}

// NewGatewayWithClients builds a Gateway against caller-supplied Clients —
// used by tests, and by nothing else.
func NewGatewayWithClients(clients map[string]Client, sink EventSink) *Gateway {
	if clients == nil {
		clients = map[string]Client{}
	}
	return &Gateway{sink: sink, overrides: clients, httpClient: &http.Client{Timeout: completionTimeout}}
}

// clientFor 现取一个 client。找不到渠道 = 这个 provider 名没有被任何已登
// 记的模型提供商占用（被删了，或者 Agent 里写错了）。
func (g *Gateway) clientFor(provider string) (Client, bool) {
	if g.overrides != nil {
		c, ok := g.overrides[provider]
		return c, ok
	}
	def, ok := providerByName(provider)
	if !ok {
		return nil, false
	}
	return def.NewClient(g.httpClient, def.DefaultBaseURL), true
}

// chainStepError names the provider so ErrAllProvidersUnavailable's
// wrapped cause is legible, without exporting a type callers would need to
// match on.
type chainStepError struct {
	provider string
	cause    error
}

func (e *chainStepError) Error() string {
	return fmt.Sprintf("provider %s: %v", e.provider, e.cause)
}
func (e *chainStepError) Unwrap() error { return e.cause }

// Complete tries primary, then each entry in fallbacks in order, stopping
// at the first provider that answers successfully. creds maps provider
// name -> the caller's decrypted Credential for it. Every failure —
// network, auth, unknown provider, missing credential — advances to the
// next link; only running out of chain is fatal
// (ErrAllProvidersUnavailable).
func (g *Gateway) Complete(ctx context.Context, primary ModelSpec, fallbacks []ModelSpec, creds map[string]Credential, req CompletionRequest) (CompletionResult, error) {
	return g.resolve(ctx, primary, fallbacks, creds, nil, func(client Client, cred Credential, spec ModelSpec) (CompletionResult, error) {
		return client.Complete(ctx, cred.APIKey, cred.BaseURL, spec.Name, applyModelParams(spec, req))
	})
}

// applyModelParams 把模型目录里为这个模型配置的参数取值并进请求，fallback
// 链的每一环按各自的 provider+model 取参——降级到另一个模型时参数跟着换。
//
// 显式的请求值优先：零值按"未设置"处理（和 buildScope 的口径一致），只有
// 未设置的字段才用模型参数补。保留名映射到类型化字段，其余进 Options。
func applyModelParams(spec ModelSpec, req CompletionRequest) CompletionRequest {
	params := modelParamsFor(spec.Provider, spec.Name)
	if len(params) == 0 {
		return req
	}
	for name, v := range params {
		switch name {
		case "max_tokens":
			if req.MaxTokens == 0 {
				if n, ok := asFloat(v); ok {
					req.MaxTokens = int(n)
				}
			}
		case "temperature":
			if req.Temperature == 0 {
				if n, ok := asFloat(v); ok {
					req.Temperature = n
				}
			}
		default:
			if req.Options == nil {
				req.Options = map[string]any{}
			}
			if _, exists := req.Options[name]; !exists {
				req.Options[name] = v
			}
		}
	}
	return req
}

// asFloat 只认 JSON 解码会产出的数字类型，不做字符串转换——参数表单里
// 填的 "8192" 在校验层就该被拦下。
func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	}
	return 0, false
}

// StreamDelta is one incremental chunk of a streaming completion — just the
// text piece a provider just produced. Tool calls are never streamed
// incrementally here (a partial function-call argument string isn't
// meaningful to display or act on); they still arrive complete on the
// terminal CompletionResult exactly as Complete already returns them.
type StreamDelta struct {
	TextDelta string
}

// StreamingClient is implemented by Clients that can speak their
// provider's token-streaming wire format. Not every provider's Client
// bothers (CompleteStream degrades gracefully via resolve when one
// doesn't), so this is a separate, optional interface rather than a method
// on Client, matching EmbeddingClient's pattern above.
type StreamingClient interface {
	CompleteStream(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest, onDelta func(StreamDelta)) (CompletionResult, error)
}

// ErrStreamAlreadyStarted is returned by CompleteStream when a provider
// failed *after* it had already streamed text to the caller. The HTTP layer
// treats it like any other completion failure; it exists as its own
// sentinel so a caller that wants to can tell "nothing was produced" apart
// from "a partial answer reached the user before this blew up".
var ErrStreamAlreadyStarted = errors.New("modelgateway: provider failed after streaming had started; not falling back")

// CompleteStream is Complete's streaming sibling: onDelta is called with
// each text chunk as the resolved provider produces it (real incremental
// deltas for a Client implementing StreamingClient; one single delta
// carrying the whole answer for one that doesn't — every caller gets
// *some* progressive callback either way, they just don't all get real
// token-level granularity). The final CompletionResult is identical in
// shape to what Complete would have returned, aggregated across the whole
// stream.
//
// 降级语义和 Complete 不同，这是有意的：**一旦有 delta 推给了调用方，这
// 一轮就不再降级。** 走 Complete 的降级链会让前端先收到半段来自 A 模型的
// 文字，再收到一整段来自 B 模型的文字，拼成一段读不通的东西——而且没有任
// 何提示说中间换过模型。半截答案比一个明确的失败更糟，后者至少可以重试。
//
// 代价是：一个在中途断流的渠道不会被兜住，用户会看到一次失败的运行。这
// 是对的取舍。还没吐出任何 delta 时（连接失败、鉴权失败、模型名不存在），
// 降级照常进行——那种情况下调用方什么都没看到，换一环是纯赚。
func (g *Gateway) CompleteStream(ctx context.Context, primary ModelSpec, fallbacks []ModelSpec, creds map[string]Credential, req CompletionRequest, onDelta func(StreamDelta)) (CompletionResult, error) {
	// onDelta 是在 call 里同步调用的（RunStream 不起 goroutine），所以这
	// 个标志位不需要加锁。
	var streamed bool
	forward := func(d StreamDelta) {
		streamed = true
		if onDelta != nil {
			onDelta(d)
		}
	}
	canFallback := func() bool { return !streamed }

	return g.resolve(ctx, primary, fallbacks, creds, canFallback, func(client Client, cred Credential, spec ModelSpec) (CompletionResult, error) {
		req := applyModelParams(spec, req)
		if sc, ok := client.(StreamingClient); ok {
			return sc.CompleteStream(ctx, cred.APIKey, cred.BaseURL, spec.Name, req, forward)
		}
		result, err := client.Complete(ctx, cred.APIKey, cred.BaseURL, spec.Name, req)
		if err == nil && result.Content != "" {
			forward(StreamDelta{TextDelta: result.Content})
		}
		return result, err
	})
}

// resolve is Complete/CompleteStream's shared fallback-chain walk: try
// primary, then each fallback in order, stopping at the first call that
// succeeds. Every failure — network, auth, unknown provider, missing
// credential — advances to the next link; only running out of chain is
// fatal (ErrAllProvidersUnavailable).
//
// canFallback 让调用方在失败后否决降级；nil 表示永远允许。流式路径用它挡
// 住"已经吐了半段文字才失败"的情况（见 CompleteStream 的注释）。
func (g *Gateway) resolve(ctx context.Context, primary ModelSpec, fallbacks []ModelSpec, creds map[string]Credential, canFallback func() bool, call func(client Client, cred Credential, spec ModelSpec) (CompletionResult, error)) (CompletionResult, error) {
	chain := make([]ModelSpec, 0, 1+len(fallbacks))
	chain = append(chain, primary)
	chain = append(chain, fallbacks...)

	var lastErr error
	for i, spec := range chain {
		if i > 0 && g.sink != nil {
			reason := "unknown error"
			if lastErr != nil {
				reason = lastErr.Error()
			}
			g.sink.EmitFallback(ctx, FallbackEvent{
				FromProvider: chain[i-1].Provider, FromModel: chain[i-1].Name,
				ToProvider: spec.Provider, ToModel: spec.Name, Reason: reason,
			})
		}

		client, ok := g.clientFor(spec.Provider)
		if !ok {
			lastErr = &chainStepError{spec.Provider, fmt.Errorf("no client configured")}
			continue
		}
		cred, ok := creds[spec.Provider]
		if !ok || cred.APIKey == "" {
			lastErr = &chainStepError{spec.Provider, fmt.Errorf("no credentials configured")}
			continue
		}

		result, err := call(client, cred, spec)
		if err != nil {
			lastErr = &chainStepError{spec.Provider, err}
			if canFallback != nil && !canFallback() {
				return CompletionResult{}, fmt.Errorf("%w: %v", ErrStreamAlreadyStarted, lastErr)
			}
			continue
		}
		result.Provider, result.Model = spec.Provider, spec.Name
		result.CostUSD = EstimateCost(spec.Provider, spec.Name, result.InputTokens, result.OutputTokens)
		return result, nil
	}
	return CompletionResult{}, fmt.Errorf("%w (last: %v)", ErrAllProvidersUnavailable, lastErr)
}

// Embed calls one provider's embeddings API — no fallback chain, since an
// embedding vector from a different model isn't a valid substitute for
// another (they aren't comparable in the same vector space), unlike a
// chat completion where any model can plausibly answer.
func (g *Gateway) Embed(ctx context.Context, spec ModelSpec, creds map[string]Credential, texts []string) ([][]float32, error) {
	client, ok := g.clientFor(spec.Provider)
	if !ok {
		return nil, fmt.Errorf("modelgateway: no client configured for provider %q", spec.Provider)
	}
	embedder, ok := client.(EmbeddingClient)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrEmbeddingsNotSupported, spec.Provider)
	}
	cred, ok := creds[spec.Provider]
	if !ok || cred.APIKey == "" {
		return nil, fmt.Errorf("modelgateway: no credentials configured for provider %q", spec.Provider)
	}
	return embedder.Embed(ctx, cred.APIKey, cred.BaseURL, spec.Name, texts)
}
