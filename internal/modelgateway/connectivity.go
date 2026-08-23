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

// NewValidator returns the Validator for a components.schemas.ProviderName
// value ("anthropic", "openai", "google", "custom"), or nil if the name is
// unrecognized.
func NewValidator(provider string) Validator {
	return newValidatorWithEndpoints(provider, providerEndpoints{})
}

func newValidatorWithEndpoints(provider string, ep providerEndpoints) Validator {
	client := &http.Client{Timeout: connectivityTimeout}
	switch provider {
	case "anthropic":
		return &anthropicValidator{client: client, baseURL: ep.anthropicBase()}
	case "openai":
		return &openaiValidator{client: client, baseURL: ep.openaiBase()}
	case "google":
		return &googleValidator{client: client, baseURL: ep.googleBase()}
	case "custom":
		// A custom provider has no known endpoint shape to probe — the
		// platform can't validate what it doesn't understand, so
		// onboarding accepts it on trust (spec-09 only requires the
		// three named providers to be connectivity-checked).
		return acceptAllValidator{}
	default:
		return nil
	}
}

type acceptAllValidator struct{}

func (acceptAllValidator) Validate(context.Context, string) error { return nil }

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

// openaiValidator probes GET /v1/models with a bearer token.
type openaiValidator struct {
	client  *http.Client
	baseURL string
}

func (v *openaiValidator) Validate(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return doAuthProbe(v.client, req)
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
