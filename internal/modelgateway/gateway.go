package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// completionTimeout bounds a single provider call. Fallback tries the next
// provider on any failure, including a timeout — a hung primary must not
// block the whole chain.
const completionTimeout = 60 * time.Second

// Message is one turn in a completion request, provider-agnostic.
type Message struct {
	Role    string // "user" | "assistant" | "system"
	Content string
}

// CompletionRequest is the platform-side shape of an LLM call; each Client
// translates it into its provider's own wire format.
type CompletionRequest struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

// CompletionResult is what every Client normalizes its provider's response
// into. Provider/Model report which link in the fallback chain actually
// answered; CostUSD is computed by the Gateway after a successful call, not
// by the Client itself (pricing isn't provider-reported).
type CompletionResult struct {
	Content      string
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
type Gateway struct {
	clients map[string]Client
	sink    EventSink
}

// NewGateway builds a Gateway with every provider in the registry
// (registry.go) — currently Anthropic/OpenAI/DeepSeek/Qwen/Google, plus a
// slot for a caller-supplied "custom" OpenAI-compatible endpoint. sink may
// be nil (no fallback events emitted, e.g. in tests that don't care).
func NewGateway(sink EventSink) *Gateway {
	return newGatewayWithEndpoints(sink, providerOverrides{})
}

func newGatewayWithEndpoints(sink EventSink, ep providerOverrides) *Gateway {
	httpClient := &http.Client{Timeout: completionTimeout}
	clients := make(map[string]Client, len(providers))
	for _, def := range providers {
		clients[def.Name] = def.NewClient(httpClient, ep.baseFor(def))
	}
	return &Gateway{sink: sink, clients: clients}
}

// NewGatewayWithClients builds a Gateway against caller-supplied Clients —
// used by tests.
func NewGatewayWithClients(clients map[string]Client, sink EventSink) *Gateway {
	return &Gateway{clients: clients, sink: sink}
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

		client, ok := g.clients[spec.Provider]
		if !ok {
			lastErr = &chainStepError{spec.Provider, fmt.Errorf("no client configured")}
			continue
		}
		cred, ok := creds[spec.Provider]
		if !ok || cred.APIKey == "" {
			lastErr = &chainStepError{spec.Provider, fmt.Errorf("no credentials configured")}
			continue
		}

		result, err := client.Complete(ctx, cred.APIKey, cred.BaseURL, spec.Name, req)
		if err != nil {
			lastErr = &chainStepError{spec.Provider, err}
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
	client, ok := g.clients[spec.Provider]
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

// ── Anthropic ────────────────────────────────────────────────────────
//
// Anthropic's wire format (content blocks, x-api-key auth) doesn't fit the
// OpenAI-compatible family below, so it stays its own small hand-rolled
// client — there is no well-established open-source Go SDK for it worth
// taking a dependency on for the one endpoint this platform calls.

type anthropicClient struct {
	client  *http.Client
	baseURL string
}

func (c *anthropicClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	base := c.baseURL
	if baseURL != "" {
		base = baseURL
	}

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature,omitempty"`
		Messages    []msg   `json:"messages"`
	}{Model: model, MaxTokens: req.MaxTokens, Temperature: req.Temperature}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, msg(m))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	status, err := postJSON(ctx, c.client, base+"/v1/messages", map[string]string{
		"x-api-key": apiKey, "anthropic-version": "2023-06-01",
	}, body, &out)
	if err != nil {
		return CompletionResult{}, err
	}
	if out.Error != nil {
		return CompletionResult{}, errors.New(out.Error.Message)
	}
	if status < 200 || status >= 300 {
		return CompletionResult{}, fmt.Errorf("anthropic: http %d", status)
	}

	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return CompletionResult{Content: text.String(), InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

// ── OpenAI-compatible family ─────────────────────────────────────────
//
// OpenAI, DeepSeek and Qwen (via DashScope's compatible-mode endpoint) all
// publish the same chat-completions wire format, and "custom" exists
// precisely so a caller can point this same client at anything else that
// does too (a self-hosted vLLM/Ollama server, an internal proxy). Built on
// github.com/sashabaranov/go-openai rather than hand-rolled JSON — the
// request/response shapes, retry-relevant status codes and the streaming
// guard are exactly the part not worth re-deriving per provider.
type openAICompatibleClient struct {
	httpClient     *http.Client
	defaultBaseURL string
	// label names the provider in error messages only; it never affects
	// the wire format, which is identical across this whole family.
	label string
}

func (c *openAICompatibleClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	base := c.defaultBaseURL
	if baseURL != "" {
		base = baseURL
	}
	if base == "" {
		return CompletionResult{}, fmt.Errorf("%s: no base_url configured", c.label)
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = strings.TrimSuffix(base, "/")
	cfg.HTTPClient = c.httpClient
	client := openai.NewClientWithConfig(cfg)

	messages := make([]openai.ChatCompletionMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, openai.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	})
	if err != nil {
		return CompletionResult{}, fmt.Errorf("%s: %w", c.label, err)
	}
	if len(resp.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("%s: no choices in response", c.label)
	}
	return CompletionResult{
		Content:      resp.Choices[0].Message.Content,
		InputTokens:  int64(resp.Usage.PromptTokens),
		OutputTokens: int64(resp.Usage.CompletionTokens),
	}, nil
}

// Embed implements EmbeddingClient. OpenAI, DeepSeek, Qwen (compatible
// mode) and any "custom" OpenAI-wire-compatible endpoint all publish the
// same POST /embeddings shape, so this is the one implementation for the
// whole family, mirroring Complete above.
func (c *openAICompatibleClient) Embed(ctx context.Context, apiKey, baseURL, model string, texts []string) ([][]float32, error) {
	base := c.defaultBaseURL
	if baseURL != "" {
		base = baseURL
	}
	if base == "" {
		return nil, fmt.Errorf("%s: no base_url configured", c.label)
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = strings.TrimSuffix(base, "/")
	cfg.HTTPClient = c.httpClient
	client := openai.NewClientWithConfig(cfg)

	resp, err := client.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: texts,
		Model: openai.EmbeddingModel(model),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.label, err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("%s: expected %d embeddings, got %d", c.label, len(texts), len(resp.Data))
	}

	// The API returns each embedding tagged with its input index, not
	// necessarily in request order — sort back into caller order rather
	// than trusting response order.
	out := make([][]float32, len(texts))
	for _, e := range resp.Data {
		out[e.Index] = e.Embedding
	}
	return out, nil
}

// ── Google ───────────────────────────────────────────────────────────

type googleClient struct {
	client  *http.Client
	baseURL string
}

func (c *googleClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	base := c.baseURL
	if baseURL != "" {
		base = baseURL
	}

	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		Contents         []content `json:"contents"`
		GenerationConfig struct {
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
			Temperature     float64 `json:"temperature,omitempty"`
		} `json:"generationConfig"`
	}{}
	body.GenerationConfig.MaxOutputTokens = req.MaxTokens
	body.GenerationConfig.Temperature = req.Temperature
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: m.Content}}})
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []part `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", base, model, apiKey)
	status, err := postJSON(ctx, c.client, url, nil, body, &out)
	if err != nil {
		return CompletionResult{}, err
	}
	if out.Error != nil {
		return CompletionResult{}, errors.New(out.Error.Message)
	}
	if status < 200 || status >= 300 {
		return CompletionResult{}, fmt.Errorf("google: http %d", status)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return CompletionResult{}, errors.New("google: no candidates in response")
	}
	return CompletionResult{
		Content:      out.Candidates[0].Content.Parts[0].Text,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
	}, nil
}

// postJSON is the shared low-level helper for the two hand-rolled clients
// (Anthropic, Google) that don't go through the go-openai SDK.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body, out any) (status int, err error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return resp.StatusCode, fmt.Errorf("non-JSON response body: %s", snippet)
	}
	return resp.StatusCode, nil
}
