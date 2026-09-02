package modelgateway

import (
	"fmt"
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/builtinchannels"
	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// ProviderDefinition is everything the Gateway and the onboarding-time
// connectivity check need to know about one model provider. It is the
// single place "add a provider" touches on the Go side.
//
// 现在它有两个来源，两者在这张表里**平权共存**：
//
//   - 声明式渠道描述符（internal/builtinchannels/*.json 与以后的插件），
//     由 descriptorProvider 转成 ProviderDefinition。加渠道 = 提一份
//     JSON，不改 Go、不发版本。
//   - 手写 client（下面的 handwritten），留给线协议怪到声明式表达得很难
//     看的厂商。这是设计内的出口，不是失败——宁可某个偏门渠道退回手写
//     Go，也不给描述符加一个"万能"算子。
type ProviderDefinition struct {
	// Name is the wire value used everywhere a provider is named:
	// ProviderName's spelling in openapi.yaml, Agent DSL's model.provider,
	// and the `provider` column on a stored credential.
	Name string
	// Label 是给人看的名字（模型提供商配置页、错误信息）。
	Label string
	// DefaultBaseURL is used when neither an explicit per-call override
	// nor a test override (providerOverrides) is set. Empty for "custom",
	// which has no documented default — every call must supply one, and a
	// call that doesn't fails at the Client/Validator rather than silently
	// reaching nothing.
	DefaultBaseURL string
	// Credentials 声明这个渠道要哪几个凭据字段，前端据此渲染表单。手写
	// client 只需要 api_key + base_url，描述符渠道由自己的 credentials[]
	// 决定。
	Credentials []CredentialFieldSpec
	// NewClient builds this provider's Client (optionally also
	// EmbeddingClient / StreamingClient, if the returned value implements
	// them) against a shared *http.Client and this provider's resolved
	// base URL.
	NewClient func(httpClient *http.Client, baseURL string) Client
	// NewValidator builds the onboarding-time credential Validator against
	// the same resolved base URL.
	NewValidator func(httpClient *http.Client, baseURL string) Validator
	// Pricing is USD per 1,000 tokens, keyed by model name. A model absent
	// here prices at $0 — EstimateCost degrades gracefully rather than
	// guessing at an unlisted model's cost.
	Pricing map[string]modelPrice
}

// CredentialFieldSpec 是一个凭据字段的形状，透给 API 和前端表单用。
type CredentialFieldSpec struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // secret | text | url
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// 所有渠道都有的两个字段。描述符没声明 credentials[] 时兜这个底。
var defaultCredentialFields = []CredentialFieldSpec{
	{Name: "api_key", Type: "secret", Label: "API Key", Required: true},
	{Name: "base_url", Type: "url", Label: "接口地址（留空用默认）", Required: false},
}

// descriptorProvider 把一份描述符转成 ProviderDefinition。Gateway、降级
// 链、计费、ADK 模型适配层一行都不用改——描述符体系整个塞在这个函数背后。
func descriptorProvider(d *descriptor.Descriptor) ProviderDefinition {
	fields := make([]CredentialFieldSpec, 0, len(d.Credentials))
	for _, f := range d.Credentials {
		fields = append(fields, CredentialFieldSpec{Name: f.Name, Type: f.Type, Label: f.Label, Required: f.Required})
	}
	if len(fields) == 0 {
		fields = defaultCredentialFields
	}
	pricing := make(map[string]modelPrice, len(d.Pricing))
	for model, p := range d.Pricing {
		pricing[model] = modelPrice{InputPer1K: p.InputPer1K, OutputPer1K: p.OutputPer1K}
	}
	return ProviderDefinition{
		Name:           d.ID,
		Label:          d.Label,
		DefaultBaseURL: d.BaseURL,
		Credentials:    fields,
		Pricing:        pricing,
		NewClient: func(hc *http.Client, base string) Client {
			return newDescriptorClient(d, hc, base)
		},
		NewValidator: func(hc *http.Client, base string) Validator {
			return &descriptorValidator{desc: d, httpClient: hc, baseURL: base}
		},
	}
}

// handwritten 是仍然用 Go 手写 client 的渠道。Google 的 wire format
// （contents/parts/functionDeclarations + key 走 query）和 OpenAI 系差得
// 远，声明式写出来会比 Go 更难读，所以留在这里——混合模式是设计的一部分。
var handwritten = []ProviderDefinition{
	{
		Name:           "google",
		Label:          "Google Gemini",
		DefaultBaseURL: "https://generativelanguage.googleapis.com",
		Credentials:    defaultCredentialFields,
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
}

// providers 是全部已注册渠道，顺序即 ProviderNames() 的顺序。
var providers = mustBuildProviders()

// mustBuildProviders 在包初始化时加载内置描述符。描述符写错了在**进程启
// 动时**就 panic，而不是等到某个用户的对话里才炸——描述符的加载期校验和
// fixtures 自检都在 builtinchannels.Load 里跑完了，能走到这里就是好的。
func mustBuildProviders() []ProviderDefinition {
	descriptors, err := builtinchannels.Load()
	if err != nil {
		panic(fmt.Sprintf("modelgateway: 内置渠道描述符加载失败: %v", err))
	}
	out := make([]ProviderDefinition, 0, len(descriptors)+len(handwritten))
	for _, d := range descriptors {
		out = append(out, descriptorProvider(d))
	}
	return append(out, handwritten...)
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

// ProviderSpec 是一个渠道对外暴露的形状：名字、显示名、默认地址、要哪几
// 个凭据字段。模型提供商配置页按它渲染表单，而不是在前端再抄一份。
type ProviderSpec struct {
	Name           string                `json:"name"`
	Label          string                `json:"label"`
	DefaultBaseURL string                `json:"default_base_url,omitempty"`
	Credentials    []CredentialFieldSpec `json:"credentials"`
}

// ProviderSpecs 返回全部渠道的对外形状。
func ProviderSpecs() []ProviderSpec {
	out := make([]ProviderSpec, 0, len(providers))
	for _, def := range providers {
		label := def.Label
		if label == "" {
			label = def.Name
		}
		out = append(out, ProviderSpec{
			Name: def.Name, Label: label,
			DefaultBaseURL: def.DefaultBaseURL,
			Credentials:    def.Credentials,
		})
	}
	return out
}

// providerOverrides lets a test point one or more providers at an httptest
// server instead of their real default endpoint, keyed by provider Name.
type providerOverrides map[string]string

// baseFor resolves a provider's base URL: its test override if one is set
// for this provider's Name, else its registered DefaultBaseURL.
func (o providerOverrides) baseFor(def ProviderDefinition) string {
	if v, ok := o[def.Name]; ok && v != "" {
		return v
	}
	return def.DefaultBaseURL
}
