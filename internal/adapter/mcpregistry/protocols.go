package mcpregistry

import (
	"fmt"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// Registry 实现 mcpsource.FetcherRegistry：按协议分发 fetcher，并把协议清
// 单透给 API 供前端渲染新建源的预设。
//
// 平台认识的协议只有两套，但能接的源不止两家：MCP 生态收敛出了一份公共的
// 注册中心接口约定，照着它实现的源（官方、各家子注册中心、自建的）全部走
// mcp-registry，靠各自的版本前缀区分。真正需要单独写 fetcher 的，只有像
// Smithery 这样自成一套的。
type Registry struct {
	fetchers map[mcpsource.Protocol]mcpsource.Fetcher
	specs    []mcpsource.ProtocolSpec
}

var _ mcpsource.FetcherRegistry = (*Registry)(nil)

func NewRegistry() *Registry {
	return &Registry{
		fetchers: map[mcpsource.Protocol]mcpsource.Fetcher{
			mcpsource.ProtocolMCPRegistry: NewRegistryFetcher(),
			mcpsource.ProtocolSmithery:    NewSmitheryFetcher(),
		},
		specs: []mcpsource.ProtocolSpec{
			{
				ID:    mcpsource.ProtocolMCPRegistry,
				Label: "MCP Registry 规范",
				Descript: "官方注册中心用的公开规范。各家子注册中心和自建注册中心" +
					"实现的是同一套接口，改一下地址和版本前缀就能接。",
				DefaultBaseURL: "https://registry.modelcontextprotocol.io",
				DefaultPrefix:  "/v0",
				DocsURL:        "https://registry.modelcontextprotocol.io/docs",
			},
			{
				ID:    mcpsource.ProtocolSmithery,
				Label: "Smithery",
				Descript: "Smithery 自成一套接口，要在它那边申请 API Key。它的列表只" +
					"说明服务器能不能远程连、不给地址，端点按托管约定拼出来；" +
					"只能本地跑的条目会标为不可接入。",
				DefaultBaseURL: "https://registry.smithery.ai",
				RequiresAPIKey: true,
				DocsURL:        "https://smithery.ai/docs/use/registry",
			},
		},
	}
}

func (r *Registry) FetcherFor(p mcpsource.Protocol) (mcpsource.Fetcher, error) {
	f, ok := r.fetchers[p]
	if !ok {
		return nil, fmt.Errorf("不认识这个源协议：%s", p)
	}
	return f, nil
}

// Protocols 返回协议清单的副本：调用方（API 层）拿去序列化，改不到这里的
// 那一份。
func (r *Registry) Protocols() []mcpsource.ProtocolSpec {
	out := make([]mcpsource.ProtocolSpec, len(r.specs))
	copy(out, r.specs)
	return out
}
