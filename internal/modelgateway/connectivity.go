// Package modelgateway is the platform's single point of contact with
// model Providers (spec-09): credential validation at onboarding, and the
// unified invocation abstraction the Bundle Orchestrator calls once a
// Bundle actually runs (task 10/11) — Agent DSL's model.provider +
// model.fallback list is resolved into a real call chain here, never in
// the orchestrator itself.
package modelgateway

import (
	"context"
	"net/http"
	"time"
)

// connectivityTimeout bounds the auth-only probe done at Provider
// onboarding time — long enough for a real API, short enough not to hang
// the create request on a slow or dead one.
const connectivityTimeout = 8 * time.Second

// Validator checks that an API key actually authenticates against a
// Provider, without spending any completion tokens.
type Validator interface {
	// Validate returns nil if apiKey authenticates successfully, and a
	// non-nil error otherwise. The error's message is safe to surface to
	// the caller (it never echoes the key itself).
	Validate(ctx context.Context, apiKey string) error
}

// providerEndpoints lets tests point a Validator/Client at an httptest
// server instead of the real vendor API; zero value uses the real one.
type providerEndpoints struct {
	AnthropicBaseURL string
	OpenAIBaseURL    string
	GoogleBaseURL    string
	DeepSeekBaseURL  string
	QwenBaseURL      string
}

func (e providerEndpoints) anthropicBase() string {
	if e.AnthropicBaseURL != "" {
		return e.AnthropicBaseURL
	}
	return "https://api.anthropic.com"
}

func (e providerEndpoints) openaiBase() string {
	if e.OpenAIBaseURL != "" {
		return e.OpenAIBaseURL
	}
	return "https://api.openai.com"
}

func (e providerEndpoints) googleBase() string {
	if e.GoogleBaseURL != "" {
		return e.GoogleBaseURL
	}
	return "https://generativelanguage.googleapis.com"
}

func (e providerEndpoints) deepseekBase() string {
	if e.DeepSeekBaseURL != "" {
		return e.DeepSeekBaseURL
	}
	return "https://api.deepseek.com/v1"
}

func (e providerEndpoints) qwenBase() string {
	if e.QwenBaseURL != "" {
		return e.QwenBaseURL
	}
	return "https://dashscope.aliyuncs.com/compatible-mode/v1"
}

// NewValidator returns the Validator for a components.schemas.ProviderName
// value ("anthropic", "openai", "google", "deepseek", "qwen", "custom"), or
// nil if the name is unrecognized. baseURL overrides the provider's
// documented default endpoint; for "custom" there is no default, so an
// empty baseURL there yields a Validator whose Validate call always fails
// (the domain Service is expected to reject a base_url-less "custom"
// registration before ever reaching here, but the Validator stays safe on
// its own).
func NewValidator(provider, baseURL string) Validator {
	return newValidatorWithEndpoints(provider, baseURL, providerEndpoints{})
}

func newValidatorWithEndpoints(provider, baseURL string, ep providerEndpoints) Validator {
	client := &http.Client{Timeout: connectivityTimeout}
	switch provider {
	case "anthropic":
		base := ep.anthropicBase()
		if baseURL != "" {
			base = baseURL
		}
		return &anthropicValidator{client: client, baseURL: base}
	case "openai":
		return &openAICompatibleValidator{client: client, baseURL: firstNonEmpty(baseURL, ep.openaiBase())}
	case "deepseek":
		return &openAICompatibleValidator{client: client, baseURL: firstNonEmpty(baseURL, ep.deepseekBase())}
	case "qwen":
		return &openAICompatibleValidator{client: client, baseURL: firstNonEmpty(baseURL, ep.qwenBase())}
	case "google":
		base := ep.googleBase()
		if baseURL != "" {
			base = baseURL
		}
		return &googleValidator{client: client, baseURL: base}
	case "custom":
		// "custom" has no documented default endpoint — the caller must
		// supply one. If it didn't, Validate deliberately fails rather than
		// silently probing nothing.
		return &openAICompatibleValidator{client: client, baseURL: baseURL}
	default:
		return nil
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// anthropicValidator probes GET /v1/models, which Anthropic's API accepts
// with just an x-api-key header and costs no completion tokens.
type anthropicValidator struct {
	client  *http.Client
	baseURL string
}

func (v *anthropicValidator) Validate(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	return doAuthProbe(v.client, req)
}

// openAICompatibleValidator probes GET {baseURL}/models with a bearer
// token, the auth scheme shared by OpenAI, DeepSeek, Qwen's DashScope
// compatible-mode endpoint, and any "custom" OpenAI-wire-compatible
// endpoint.
type openAICompatibleValidator struct {
	client  *http.Client
	baseURL string
}

func (v *openAICompatibleValidator) Validate(ctx context.Context, apiKey string) error {
	if v.baseURL == "" {
		return &validationError{"no base_url configured for this provider"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimTrailingSlash(v.baseURL)+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return doAuthProbe(v.client, req)
}

func trimTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}

// googleValidator probes GET /v1beta/models with the key as a query
// parameter, per the Generative Language API's auth scheme.
type googleValidator struct {
	client  *http.Client
	baseURL string
}

func (v *googleValidator) Validate(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/v1beta/models?key="+apiKey, nil)
	if err != nil {
		return err
	}
	return doAuthProbe(v.client, req)
}

// ErrCredentialsInvalid is returned when the Provider reached us and
// explicitly rejected the key (401/403) — as opposed to a network failure,
// which is reported as its own error so the caller can tell "your key is
// wrong" apart from "we couldn't reach the Provider at all".
var ErrCredentialsInvalid = &validationError{"provider rejected the credentials"}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func doAuthProbe(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return &validationError{"could not reach provider: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrCredentialsInvalid
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	default:
		return &validationError{"provider returned unexpected status"}
	}
}
