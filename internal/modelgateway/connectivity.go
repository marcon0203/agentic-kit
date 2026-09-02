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

// NewValidator returns the Validator for a components.schemas.ProviderName
// value ("deepseek", "volcengine", "qwen", "custom", "google", or any
// provider appended to registry.go's providers / added as a channel
// descriptor), or nil if the name isn't registered. baseURL overrides the provider's documented default
// endpoint; for "custom" there is no default, so an empty baseURL there
// yields a Validator whose Validate call always fails (the domain Service
// is expected to reject a base_url-less "custom" registration before ever
// reaching here, but the Validator stays safe on its own).
func NewValidator(provider, baseURL string) Validator {
	return newValidatorWithEndpoints(provider, baseURL, providerOverrides{})
}

func newValidatorWithEndpoints(provider, baseURL string, ep providerOverrides) Validator {
	def, ok := providerByName(provider)
	if !ok {
		return nil
	}
	base := ep.baseFor(def)
	if baseURL != "" {
		base = baseURL
	}
	return def.NewValidator(&http.Client{Timeout: connectivityTimeout}, base)
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
