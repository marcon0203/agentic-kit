package modelgateway

import (
	"net/http"
	"sort"
	"sync"

	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// ProviderDefinition is everything the Gateway and the onboarding-time
// connectivity check need to know about one model channel.
//
// 注册表是**运行时可变**的：渠道由管理员在 系统配置 → 模型提供商 里从协议
// 模板创建，描述符落在 catalog_providers.descriptor，进程启动时加载、增删
// 改后重载（SetChannels）。平台开箱**不带任何渠道**——模型供应商是部署方
// 的配置，不是平台的产品内容。
type ProviderDefinition struct {
	// Name is the wire value used everywhere a channel is named: Agent
	// DSL's model.provider, the `provider` column on a stored credential,
	// and catalog_providers.provider_key.
	Name string
	// Label 是给人看的名字（模型提供商配置页、错误信息）。
	Label string
	// DefaultBaseURL 是这个渠道的接口地址。描述符渠道由创建时填的
	// base_url 决定，用户的个人凭据里可以再覆盖。
	DefaultBaseURL string
	// Credentials 声明这个渠道要哪几个凭据字段，前端据此渲染表单。
	Credentials []CredentialFieldSpec
	// NewClient builds this channel's Client (optionally also
	// EmbeddingClient / StreamingClient, if the returned value implements
	// them) against a shared *http.Client and this channel's resolved base
	// URL.
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
	{Name: "base_url", Type: "url", Label: "接口地址（留空用渠道默认）", Required: false},
}

// registry 是进程内唯一的渠道注册表。读多写少（写只发生在启动和管理员改
// 配置时），用 RWMutex 而不是 atomic swap 是因为读侧要遍历取名字列表。
type registry struct {
	mu    sync.RWMutex
	items []ProviderDefinition
	// modelParams 是每渠道每模型的请求参数取值（provider -> model ->
	// params）。和 items 一样由管理员的写操作触发整体重建；两个 Set 分开
	// 是因为数据来源不同（描述符快照 vs catalog_models.params），但同一次
	// Reload 里先后调用，读侧按 provider+model 现查现用，不存在谁等谁。
	modelParams map[string]map[string]map[string]any
}

var channels = &registry{}

// SetChannels 整体替换注册表。整体替换而不是增量增删：管理员的每次改动之
// 后都从库里重新读一遍全量，注册表和库不会出现"某次删除漏掉了"的偏差。
func SetChannels(descriptors []*descriptor.Descriptor) {
	items := make([]ProviderDefinition, 0, len(descriptors))
	for _, d := range descriptors {
		items = append(items, descriptorProvider(d))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	channels.mu.Lock()
	channels.items = items
	channels.mu.Unlock()
}

// SetModelParams 整体替换模型参数表，来源是模型目录（catalog_models.params）。
// 没有参数的模型不出现在表里——applyModelParams 对查不到的模型原样放行，
// 是否缺必填参数由 descriptorClient 按渠道声明拦。
func SetModelParams(params map[string]map[string]map[string]any) {
	channels.mu.Lock()
	channels.modelParams = params
	channels.mu.Unlock()
}

// modelParamsFor 取一个模型的参数取值，没有就是 nil。
func modelParamsFor(provider, model string) map[string]any {
	channels.mu.RLock()
	defer channels.mu.RUnlock()
	if channels.modelParams == nil {
		return nil
	}
	if models, ok := channels.modelParams[provider]; ok {
		return models[model]
	}
	return nil
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

// providerByName looks up a registered channel by its wire name.
func providerByName(name string) (ProviderDefinition, bool) {
	channels.mu.RLock()
	defer channels.mu.RUnlock()
	for _, def := range channels.items {
		if def.Name == name {
			return def, true
		}
	}
	return ProviderDefinition{}, false
}

// ProviderNames returns every registered channel's Name, sorted.
// modelcenter.KnownProviders and this package's own validation error
// messages read from this instead of maintaining a second list.
func ProviderNames() []string {
	channels.mu.RLock()
	defer channels.mu.RUnlock()
	names := make([]string, len(channels.items))
	for i, def := range channels.items {
		names[i] = def.Name
	}
	return names
}

// ProviderSpec 是一个渠道对外暴露的形状：名字、显示名、默认地址、要哪几
// 个凭据字段。模型提供商配置页和模型广场的接入弹窗按它渲染表单，而不是在
// 前端再抄一份渠道列表。
type ProviderSpec struct {
	Name           string                `json:"name"`
	Label          string                `json:"label"`
	DefaultBaseURL string                `json:"default_base_url,omitempty"`
	Credentials    []CredentialFieldSpec `json:"credentials"`
}

// ProviderSpecs 返回全部渠道的对外形状。
func ProviderSpecs() []ProviderSpec {
	channels.mu.RLock()
	defer channels.mu.RUnlock()
	out := make([]ProviderSpec, 0, len(channels.items))
	for _, def := range channels.items {
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

// providerOverrides lets a test point one or more channels at an httptest
// server instead of their registered endpoint, keyed by channel Name.
type providerOverrides map[string]string

// baseFor resolves a channel's base URL: its test override if one is set,
// else its registered DefaultBaseURL.
func (o providerOverrides) baseFor(def ProviderDefinition) string {
	if v, ok := o[def.Name]; ok && v != "" {
		return v
	}
	return def.DefaultBaseURL
}
