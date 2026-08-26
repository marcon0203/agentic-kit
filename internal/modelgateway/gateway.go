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

// anthropicTool is Anthropic's tool-definition wire shape
// (https://docs.anthropic.com/en/docs/build-with-claude/tool-use):
// input_schema is a standard JSON Schema object, taken as-is from
// Tool.InputSchema.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicMessage's Content is either a plain string (ordinary text turns)
// or a []map[string]any of content blocks (tool_use/tool_result turns) —
// Anthropic accepts both shapes, so building whichever is simplest per
// message avoids a content-block wrapper for the common all-text case.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// buildAnthropicMessages translates the platform's provider-agnostic
// Messages into Anthropic's turn shape. "system" messages are excluded —
// Anthropic takes the system prompt as a separate top-level field, never a
// message role, so the caller pulls it out via extractSystemPrompt first
// and passes the rest here.
func buildAnthropicMessages(msgs []Message) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "tool":
			out = append(out, anthropicMessage{Role: "user", Content: []map[string]any{{
				"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content,
			}}})
		case "assistant":
			if len(m.ToolCalls) == 0 {
				out = append(out, anthropicMessage{Role: "assistant", Content: m.Content})
				continue
			}
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": tc.Arguments})
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})
		default:
			out = append(out, anthropicMessage{Role: "user", Content: m.Content})
		}
	}
	return out
}

// extractSystemPrompt concatenates every "system" message's Content —
// Anthropic (and Google) take one system prompt string, not a role inside
// the turn sequence.
func extractSystemPrompt(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != "system" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

func (c *anthropicClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	base := c.baseURL
	if baseURL != "" {
		base = baseURL
	}

	body := struct {
		Model       string             `json:"model"`
		MaxTokens   int                `json:"max_tokens"`
		Temperature float64            `json:"temperature,omitempty"`
		System      string             `json:"system,omitempty"`
		Messages    []anthropicMessage `json:"messages"`
		Tools       []anthropicTool    `json:"tools,omitempty"`
	}{
		Model: model, MaxTokens: req.MaxTokens, Temperature: req.Temperature,
		System: extractSystemPrompt(req.Messages), Messages: buildAnthropicMessages(req.Messages),
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}

	var out struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
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
	var toolCalls []ToolCall
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
		}
	}
	return CompletionResult{
		Content: text.String(), ToolCalls: toolCalls,
		InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens,
	}, nil
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
		msg := openai.ChatCompletionMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			args, _ := json.Marshal(tc.Arguments)
			msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
				ID: tc.ID, Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{Name: tc.Name, Arguments: string(args)},
			})
		}
		messages = append(messages, msg)
	}

	var tools []openai.Tool
	for _, t := range req.Tools {
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name: t.Name, Description: t.Description, Parameters: t.InputSchema,
			},
		})
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	})
	if err != nil {
		return CompletionResult{}, fmt.Errorf("%s: %w", c.label, err)
	}
	if len(resp.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("%s: no choices in response", c.label)
	}

	var toolCalls []ToolCall
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		toolCalls = append(toolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
	}
	return CompletionResult{
		Content: resp.Choices[0].Message.Content, ToolCalls: toolCalls,
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

// googlePart covers the three part shapes this client speaks: plain text,
// a model-issued function call, and the function's result being replayed
// back. Gemini correlates a functionResponse to its functionCall by name
// only — there is no id field in this wire format, unlike Anthropic/OpenAI.
type googlePart struct {
	Text             string          `json:"text,omitempty"`
	FunctionCall     *googleFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *googleFuncResp `json:"functionResponse,omitempty"`
}
type googleFuncCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}
type googleFuncResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
}
type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}
type googleFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// buildGoogleContents translates the platform's provider-agnostic Messages
// into Gemini's turn shape. "system" messages are excluded — Gemini takes
// the system prompt as a separate top-level systemInstruction, never a
// role inside contents[].
func buildGoogleContents(msgs []Message) []googleContent {
	out := make([]googleContent, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "tool":
			out = append(out, googleContent{Role: "user", Parts: []googlePart{{
				FunctionResponse: &googleFuncResp{Name: m.ToolName, Response: map[string]any{"output": m.Content}},
			}}})
		case "assistant":
			var parts []googlePart
			if m.Content != "" {
				parts = append(parts, googlePart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, googlePart{FunctionCall: &googleFuncCall{Name: tc.Name, Args: tc.Arguments}})
			}
			out = append(out, googleContent{Role: "model", Parts: parts})
		default:
			out = append(out, googleContent{Role: "user", Parts: []googlePart{{Text: m.Content}}})
		}
	}
	return out
}

func (c *googleClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	base := c.baseURL
	if baseURL != "" {
		base = baseURL
	}

	body := struct {
		Contents          []googleContent `json:"contents"`
		SystemInstruction *googleContent  `json:"systemInstruction,omitempty"`
		Tools             []struct {
			FunctionDeclarations []googleFunctionDeclaration `json:"functionDeclarations"`
		} `json:"tools,omitempty"`
		GenerationConfig struct {
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
			Temperature     float64 `json:"temperature,omitempty"`
		} `json:"generationConfig"`
	}{Contents: buildGoogleContents(req.Messages)}
	body.GenerationConfig.MaxOutputTokens = req.MaxTokens
	body.GenerationConfig.Temperature = req.Temperature
	if sys := extractSystemPrompt(req.Messages); sys != "" {
		body.SystemInstruction = &googleContent{Parts: []googlePart{{Text: sys}}}
	}
	if len(req.Tools) > 0 {
		decls := make([]googleFunctionDeclaration, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, googleFunctionDeclaration{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
		}
		body.Tools = append(body.Tools, struct {
			FunctionDeclarations []googleFunctionDeclaration `json:"functionDeclarations"`
		}{FunctionDeclarations: decls})
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
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

	var text strings.Builder
	var toolCalls []ToolCall
	for _, p := range out.Candidates[0].Content.Parts {
		if p.FunctionCall != nil {
			toolCalls = append(toolCalls, ToolCall{Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args})
			continue
		}
		text.WriteString(p.Text)
	}
	return CompletionResult{
		Content: text.String(), ToolCalls: toolCalls,
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
