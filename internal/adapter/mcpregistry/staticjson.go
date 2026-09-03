package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// StaticJSONFetcher 把"一个返回 JSON 清单的地址"当成源。
//
// 存在的理由是国内这几家：腾讯云 MCP 广场、阿里云百炼、百度千帆、魔搭的
// MCP 广场都没有公开的列表接口，服务清单只在各自控制台里登录可见。想把它
// 们的服务纳进本平台的同步→审核→市场→接入这条链路，只能由管理员把清单落
// 成一份 JSON——托在内网、对象存储、甚至 Git 仓库的 raw 地址都行。
//
// 清单用的就是官方 server.json 的形状，两种写法都认：
//
//	{"servers": [ {...}, {...} ]}
//	[ {...}, {...} ]
//
// 于是这个协议顺带还有两个用处：内部自研的 MCP Server 不用为了上架先搭一
// 个注册中心；哪天上面某一家开放了列表接口，把源改成 mcp-registry 协议就
// 行，条目和审核结论都不用动（slug 不变）。
type StaticJSONFetcher struct {
	http *httpGetter
}

var _ mcpsource.Fetcher = (*StaticJSONFetcher)(nil)

func NewStaticJSONFetcher() *StaticJSONFetcher {
	return &StaticJSONFetcher{http: newHTTPGetter()}
}

// FetchList 拉一次清单。没有分页——一份手工维护的清单不会大到需要翻页，真
// 大到那个程度的话，对方多半也已经有接口了。
//
// 地址就是 base_url + api_prefix 原样拼接，不追加 /servers：这个协议指向的
// 是一个具体文件（.../mcp-servers.json），不是一套接口。
func (f *StaticJSONFetcher) FetchList(ctx context.Context, target mcpsource.FetchTarget) ([]mcpsource.FetchedServer, error) {
	u := target.BaseURL + target.APIPrefix

	// 先当成原始字节收下来，再决定按哪种形状解析——两种写法都得认，而
	// json.Unmarshal 到具体类型上会因为另一种直接报错。
	var raw json.RawMessage
	if err := f.http.getJSON(ctx, u, target.APIKey, &raw); err != nil {
		return nil, err
	}

	var entries []wireEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		var wrapped wireListResp
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			return nil, fmt.Errorf("清单既不是 servers 数组也不是 {\"servers\":[...]}：%w", err)
		}
		entries = wrapped.Servers
	}
	if len(entries) == 0 {
		// 空清单和"地址填错了指到一个别的 JSON 上"看起来一样。宁可报错，也
		// 不要静默地把整个源的缓存清空。
		return nil, fmt.Errorf("清单里一个条目都没有，确认地址指向的是 MCP 服务清单")
	}

	out := make([]mcpsource.FetchedServer, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		s := entry.resolve()
		if s.Name == "" || seen[s.Name] {
			continue
		}
		if s.Status != "" && s.Status != "active" {
			continue
		}
		seen[s.Name] = true
		out = append(out, fetchedFromWire(s))
	}
	return out, nil
}
