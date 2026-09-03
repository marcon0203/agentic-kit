package mcpsource

import "strings"

// Protocol 说的是"这个源的接口长什么样"，决定同步时用哪个 fetcher 解析
// 上游响应。它不是"这是哪一家"——PulseMCP 之类的子注册中心实现的是官方那
// 套规范，和官方共用 ProtocolMCPRegistry，只是接口前缀不同。
//
// 按"协议"而不是按"厂商"分类是有意的：MCP 生态已经收敛到一份公共的注册中
// 心规范（server.json + remotes/packages），照着它实现的源不该各写一份
// fetcher。真正需要单独实现的只有自成一套的那几家。
type Protocol string

const (
	// ProtocolMCPRegistry 是官方 MCP Registry 规范，也是各家子注册中心和
	// 自建注册中心的通用协议。
	ProtocolMCPRegistry Protocol = "mcp-registry"
	// ProtocolSmithery 是 Smithery 自己的一套（qualifiedName + pagination），
	// 且要 API Key 才给读。
	ProtocolSmithery Protocol = "smithery"
)

// ProtocolSpec 是一个协议对外暴露的形状：怎么称呼它、默认接口前缀是什么、
// 要不要密钥。新建源的弹窗按它渲染预设和表单，前端不再抄第二份协议清单
// ——和模型渠道的 ProviderSpecs 是同一个约定。
type ProtocolSpec struct {
	ID       Protocol `json:"id"`
	Label    string   `json:"label"`
	Descript string   `json:"description,omitempty"`
	// DefaultBaseURL 为空表示"这个协议没有公认的站点，得管理员自己填"。
	DefaultBaseURL string `json:"default_base_url,omitempty"`
	DefaultPrefix  string `json:"default_api_prefix,omitempty"`
	RequiresAPIKey bool   `json:"requires_api_key"`
	// DocsURL 指向对方的接口文档。填错前缀是这套东西最容易出的错，给个能
	// 点开去核对的地方比在提示文案里解释便宜。
	DocsURL string `json:"docs_url,omitempty"`
}

// FetchTarget 是同步一个源要交给 fetcher 的全部信息。做成结构体而不是三
// 个参数：以后再加字段（比如自定义请求头）不用动每个 fetcher 的签名。
type FetchTarget struct {
	BaseURL   string
	APIPrefix string
	// APIKey 是已解密的明文，只在同步这一次调用里存在，不落任何日志。
	APIKey string
}

// FetcherRegistry 按协议分发 fetcher，并把协议清单透给 API。领域层只认这
// 个端口，具体哪几家、各家怎么解析全在 adapter 层。
type FetcherRegistry interface {
	FetcherFor(protocol Protocol) (Fetcher, error)
	Protocols() []ProtocolSpec
}

// normalizeAPIPrefix 统一前缀写法：允许管理员填 "v0"、"/v0"、"/v0/"，都
// 收敛成 "/v0"。空串是合法的（Smithery 这类把版本写在 host 里的源）。
func normalizeAPIPrefix(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	return "/" + trimmed
}
