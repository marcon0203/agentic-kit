package modelgateway

import "net/http"

// ProviderDefinition is everything the Gateway and the onboarding-time
// connectivity check need to know about one model provider. It is the
// single place "add a provider" touches on the Go side — before this
// registry existed, a new provider meant editing five separate switches
// (the client map in newGatewayWithEndpoints, a default-base-URL helper on
// providerEndpoints, a case in connectivity.go's Validator switch, an
// entry in pricing.go's table, and modelcenter.KnownProviders). Now it's
// one entry appended to providers below.
type ProviderDefinition struct {
	// Name is the wire value used everywhere a provider is named:
	// ProviderName's spelling in openapi.yaml, Agent DSL's model.provider,
	// and the `provider` column on a stored credential.
	Name string
	// DefaultBaseURL is used when neither an explicit per-call override
	// nor a test override (providerOverrides) is set. Empty for "custom",
	// which has no documented default — every call must supply one, and a
	// call that doesn't fails at the Client/Validator rather than silently
	// reaching nothing.
	DefaultBaseURL string
	// NewClient builds this provider's Client (optionally also
	// EmbeddingClient, if the returned value implements it) against a
	// shared *http.Client and this provider's resolved base URL.
	NewClient func(httpClient *http.Client, baseURL string) Client
	// NewValidator builds the onboarding-time credential Validator against
	// the same resolved base URL.
	NewValidator func(httpClient *http.Client, baseURL string) Validator
	// Pricing is USD per 1,000 tokens, keyed by model name. A model absent
	// here prices at $0 — EstimateCost degrades gracefully rather than
	// guessing at an unlisted model's cost.
	Pricing map[string]modelPrice
}

func openAICompatibleFamily(label, defaultBaseURL string) ProviderDefinition {
	return ProviderDefinition{
		Name:           label,
		DefaultBaseURL: defaultBaseURL,
		NewClient: func(hc *http.Client, base string) Client {
			return &openAICompatibleClient{httpClient: hc, defaultBaseURL: base, label: label}
		},
		NewValidator: func(hc *http.Client, base string) Validator {
			return &openAICompatibleValidator{client: hc, baseURL: base}
		},
	}
}

// providers is the registry every built-in provider is defined in, in the
// order ProviderNames() reports them. OpenAI, DeepSeek and Qwen share the
// same wire format (see openAICompatibleClient's doc comment) so they're
// built via openAICompatibleFamily; Anthropic and Google speak their own
// formats and get hand-rolled Client/Validator pairs. "custom" is the
// escape hatch for any other OpenAI-wire-compatible endpoint (a
// self-hosted vLLM/Ollama server, an internal proxy) without needing a
// code change here at all.
var providers = []ProviderDefinition{
	{
		Name:           "anthropic",
		DefaultBaseURL: "https://api.anthropic.com",
		NewClient: func(hc *http.Client, base string) Client {
			return &anthropicClient{client: hc, baseURL: base}
		},
		NewValidator: func(hc *http.Client, base string) Validator {
			return &anthropicValidator{client: hc, baseURL: base}
		},
		Pricing: map[string]modelPrice{
			"claude-sonnet-5":  {InputPer1K: 0.003, OutputPer1K: 0.015},
			"claude-opus-5":    {InputPer1K: 0.015, OutputPer1K: 0.075},
			"claude-haiku-4-5": {InputPer1K: 0.0008, OutputPer1K: 0.004},
		},
	},
	func() ProviderDefinition {
		def := openAICompatibleFamily("openai", "https://api.openai.com")
		def.Pricing = map[string]modelPrice{
			"gpt-4o":      {InputPer1K: 0.0025, OutputPer1K: 0.01},
			"gpt-4o-mini": {InputPer1K: 0.00015, OutputPer1K: 0.0006},
		}
		return def
	}(),
	{
		Name:           "google",
		DefaultBaseURL: "https://generativelanguage.googleapis.com",
		NewClient: func(hc *http.Client, base string) Client {
			return &googleClient{client: hc, baseURL: base}
		},
		NewValidator: func(hc *http.Client, base string) Validator {
			return &googleValidator{client: hc, baseURL: base}
		},
		Pricing: map[string]modelPrice{
			"gemini-1.5-pro":   {InputPer1K: 0.00125, OutputPer1K: 0.005},
			"gemini-1.5-flash": {InputPer1K: 0.000075, OutputPer1K: 0.0003},
		},
	},
	openAICompatibleFamily("deepseek", "https://api.deepseek.com/v1"),
	openAICompatibleFamily("qwen", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
	openAICompatibleFamily("custom", ""),
}

// providerByName looks up a registered ProviderDefinition by its wire name.
func providerByName(name string) (ProviderDefinition, bool) {
	for _, def := range providers {
		if def.Name == name {
			return def, true
		}
	}
	return ProviderDefinition{}, false
}

// ProviderNames returns every registered provider's Name, in registration
// order. modelcenter.KnownProviders and this package's own validation
// error messages read from this instead of maintaining a second hardcoded
// list that could drift out of sync with the registry.
func ProviderNames() []string {
	names := make([]string, len(providers))
	for i, def := range providers {
		names[i] = def.Name
	}
	return names
}

// providerOverrides lets a test point one or more providers at an httptest
// server instead of their real default endpoint, keyed by provider Name.
// Replaces the old providerEndpoints struct-of-named-fields, which needed
// a new field (and two new accessor methods) for every provider added —
// the same maintenance burden this whole registry exists to remove.
type providerOverrides map[string]string

// baseFor resolves a provider's base URL: its test override if one is set
// for this provider's Name, else its registered DefaultBaseURL.
func (o providerOverrides) baseFor(def ProviderDefinition) string {
	if v, ok := o[def.Name]; ok && v != "" {
		return v
	}
	return def.DefaultBaseURL
}
