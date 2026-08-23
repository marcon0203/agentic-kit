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

// Client speaks one provider's completion API.
type Client interface {
	Complete(ctx context.Context, apiKey, model string, req CompletionRequest) (CompletionResult, error)
}

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

// NewGateway builds a Gateway against the real Anthropic/OpenAI/Google
// APIs. sink may be nil (no fallback events emitted, e.g. in tests that
// don't care).
func NewGateway(sink EventSink) *Gateway {
	return newGatewayWithEndpoints(sink, providerEndpoints{})
}

func newGatewayWithEndpoints(sink EventSink, ep providerEndpoints) *Gateway {
	httpClient := &http.Client{Timeout: completionTimeout}
	return &Gateway{
		sink: sink,
		clients: map[string]Client{
			"anthropic": &anthropicClient{client: httpClient, baseURL: ep.anthropicBase()},
			"openai":    &openaiClient{client: httpClient, baseURL: ep.openaiBase()},
			"google":    &googleClient{client: httpClient, baseURL: ep.googleBase()},
		},
	}
}

// NewGatewayWithClients builds a Gateway against caller-supplied Clients —
// used by tests, and available to callers who want a "custom" provider
// wired in.
func NewGatewayWithClients(clients map[string]Client, sink EventSink) *Gateway {
	return &Gateway{clients: clients, sink: sink}
}

// apiKeyNotConfiguredError names the provider so ErrAllProvidersUnavailable's
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
// at the first provider that answers successfully. apiKeys maps provider
// name -> decrypted credential. Every failure — network, auth, unknown
// provider, missing credential — advances to the next link; only running
// out of chain is fatal (ErrAllProvidersUnavailable).
func (g *Gateway) Complete(ctx context.Context, primary ModelSpec, fallbacks []ModelSpec, apiKeys map[string]string, req CompletionRequest) (CompletionResult, error) {
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
		apiKey, ok := apiKeys[spec.Provider]
		if !ok || apiKey == "" {
			lastErr = &chainStepError{spec.Provider, fmt.Errorf("no credentials configured")}
			continue
		}

		result, err := client.Complete(ctx, apiKey, spec.Name, req)
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

// ── Anthropic ────────────────────────────────────────────────────────

type anthropicClient struct {
	client  *http.Client
	baseURL string
}

func (c *anthropicClient) Complete(ctx context.Context, apiKey, model string, req CompletionRequest) (CompletionResult, error) {
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
	status, err := postJSON(ctx, c.client, c.baseURL+"/v1/messages", map[string]string{
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

// ── OpenAI ───────────────────────────────────────────────────────────

type openaiClient struct {
	client  *http.Client
	baseURL string
}

func (c *openaiClient) Complete(ctx context.Context, apiKey, model string, req CompletionRequest) (CompletionResult, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens,omitempty"`
		Temperature float64 `json:"temperature,omitempty"`
		Messages    []msg   `json:"messages"`
	}{Model: model, MaxTokens: req.MaxTokens, Temperature: req.Temperature}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, msg(m))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	status, err := postJSON(ctx, c.client, c.baseURL+"/v1/chat/completions", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, body, &out)
	if err != nil {
		return CompletionResult{}, err
	}
	if out.Error != nil {
		return CompletionResult{}, errors.New(out.Error.Message)
	}
	if status < 200 || status >= 300 {
		return CompletionResult{}, fmt.Errorf("openai: http %d", status)
	}
	if len(out.Choices) == 0 {
		return CompletionResult{}, errors.New("openai: no choices in response")
	}
	return CompletionResult{
		Content:     out.Choices[0].Message.Content,
		InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens,
	}, nil
}

// ── Google ───────────────────────────────────────────────────────────

type googleClient struct {
	client  *http.Client
	baseURL string
}

func (c *googleClient) Complete(ctx context.Context, apiKey, model string, req CompletionRequest) (CompletionResult, error) {
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
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.baseURL, model, apiKey)
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

// ── shared HTTP helper ───────────────────────────────────────────────

// postJSON sends a JSON POST and decodes the response body into out
// regardless of status code — every Client's out struct carries an
// optional Error field for the provider's own error shape, which the
// caller checks after this returns. The returned status is what the
// caller falls back to (a plain "http 401") when the body didn't carry a
// structured error it recognizes.
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
