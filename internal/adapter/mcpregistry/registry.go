// Package mcpregistry 实现 MCP 源的各家上游协议。它住在 adapter 层是因为
// domain 不允许碰 net/http（见 domain/layering_test）——协议细节全部藏在这
// 里，mcpsource.Service 只面对 Fetcher / FetcherRegistry 两个接口。
//
// 目前两套：
//
//   - RegistryFetcher（本文件）：官方 MCP Registry 规范。MCP 生态收敛出了
//     一份公共的注册中心接口约定（server.json + remotes/packages），官方和
//     各家子注册中心说的是同一套，差别只在版本前缀，所以它们共用这一个实
//     现，前缀由源自己带（FetchTarget.APIPrefix）。
//   - SmitheryFetcher（smithery.go）：Smithery 自成一套，且要 API Key。
package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// RegistryFetcher 按官方 MCP Registry 规范拉取。字段宽进严出：缺
// description、缺 repository 都不报错，落库时留空。
type RegistryFetcher struct {
	http *httpGetter
	// maxPages 限制单次同步的分页深度，防止一个异常源把同步挂死。
	maxPages int
	pageSize int
}

var _ mcpsource.Fetcher = (*RegistryFetcher)(nil)

func NewRegistryFetcher() *RegistryFetcher {
	return &RegistryFetcher{http: newHTTPGetter(), maxPages: 40, pageSize: 100}
}

// ── 上游 wire 结构 ────────────────────────────────────────────────────
//
// 注册中心的响应换过一次形状：条目早期是平铺的，后来被包进 {"server": …}
// 并把发布元数据挪进 _meta。两种都在野外跑着（自建注册中心的版本参差不
// 齐），所以两种都认——见 wireEntry.server()。

type wireRepository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

type wireRemote struct {
	Type string `json:"type"` // streamable-http | sse
	URL  string `json:"url"`
}

// wireIcon 是 server.json 里 icons[] 的一项。规范给的是一组不同尺寸的图，
// 我们只需要一个能显示的地址。
type wireIcon struct {
	Src      string `json:"src"`
	MimeType string `json:"mimeType"`
	Sizes    string `json:"sizes"`
}

type wireServer struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Version     string          `json:"version"`
	Repository  *wireRepository `json:"repository"`
	Remotes     []wireRemote    `json:"remotes"`
	// 图标的三种写法都认：规范里的 icons[]，以及各家子注册中心常见的两个
	// 单值字段。宽进严出——多认几个字段不花什么代价，认漏了就是一整片空
	// 白卡片。
	Icons   []wireIcon `json:"icons"`
	IconURL string     `json:"iconUrl"`
	LogoURL string     `json:"logoUrl"`
	// Packages 只用来判断"这条目是不是只能本地跑"，内容原样留在 raw 里。
	Packages []json.RawMessage `json:"packages"`
	Meta     wireMeta          `json:"_meta"`
}

type wireOfficialMeta struct {
	PublishedAt string `json:"publishedAt"`
	UpdatedAt   string `json:"updatedAt"`
	IsLatest    *bool  `json:"isLatest"`
}

type wireMeta struct {
	Official *wireOfficialMeta `json:"io.modelcontextprotocol.registry/official"`
}

type wireEntry struct {
	wireServer
	Server *wireServer `json:"server"`
	Meta   wireMeta    `json:"_meta"`
}

// resolve 抹平两种条目形状，并把外层 _meta（新形状把发布元数据放在条目
// 上而不是 server 里）并回来。
func (e wireEntry) resolve() wireServer {
	s := e.wireServer
	if e.Server != nil {
		s = *e.Server
	}
	if s.Meta.Official == nil && e.Meta.Official != nil {
		s.Meta = e.Meta
	}
	return s
}

type wireMetadata struct {
	NextCursor      string `json:"nextCursor"`
	NextCursorSnake string `json:"next_cursor"`
}

func (m wireMetadata) cursor() string {
	if m.NextCursor != "" {
		return m.NextCursor
	}
	return m.NextCursorSnake
}

type wireListResp struct {
	Servers  []wireEntry  `json:"servers"`
	Metadata wireMetadata `json:"metadata"`
}

// parseTime 认 RFC3339（注册中心用的格式）；解析不了就当上游没给。
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// pickIcon 从三种写法里挑一个图标地址。icons[] 里优先挑一个位图（有
// mimeType 的），实在没有就用第一个有 src 的。
func pickIcon(s wireServer) string {
	for _, ic := range s.Icons {
		if ic.Src != "" && ic.MimeType != "" {
			return ic.Src
		}
	}
	for _, ic := range s.Icons {
		if ic.Src != "" {
			return ic.Src
		}
	}
	if s.IconURL != "" {
		return s.IconURL
	}
	return s.LogoURL
}

// pickRemote 选一个远端地址。同一个 Server 可能同时给 streamable-http 和
// sse 两个入口，优先前者：sse 是上一代传输，新服务端只保证 streamable-http
// 可用。
func pickRemote(remotes []wireRemote) (url, typ string) {
	for _, r := range remotes {
		if r.URL != "" && r.Type == "streamable-http" {
			return r.URL, r.Type
		}
	}
	for _, r := range remotes {
		if r.URL != "" {
			return r.URL, r.Type
		}
	}
	return "", ""
}

// fetchedFromWire 把一条 server.json 转成领域条目。抽出来是因为
// static-json 协议读的是同一种形状的清单，两边必须转得一模一样——否则同一
// 个服务从注册中心同步和从清单同步，落库的字段会不一致。
func fetchedFromWire(s wireServer) mcpsource.FetchedServer {
	remoteURL, remoteType := pickRemote(s.Remotes)
	raw, _ := json.Marshal(s)
	fs := mcpsource.FetchedServer{
		Slug:       s.Name,
		Name:       s.Name,
		Summary:    s.Description,
		Version:    s.Version,
		RemoteURL:  remoteURL,
		RemoteType: remoteType,
		IconURL:    pickIcon(s),
		Topics:     topicsOf(s, remoteType),
		Raw:        raw,
	}
	if s.Repository != nil {
		fs.RepositoryURL = s.Repository.URL
	}
	if s.Meta.Official != nil {
		fs.UpdatedAt = parseTime(s.Meta.Official.UpdatedAt)
		if fs.UpdatedAt.IsZero() {
			fs.UpdatedAt = parseTime(s.Meta.Official.PublishedAt)
		}
	}
	return fs
}

// topicsOf 给条目打上页面上能筛的标签：有没有远端地址、代码托管在哪。
// 注册中心本身不给分类字段，这两个恰好是用户最先要问的问题（"我现在能不
// 能接"、"这是谁发的"）。
func topicsOf(s wireServer, remoteType string) []string {
	topics := make([]string, 0, 2)
	if remoteType != "" {
		topics = append(topics, remoteType)
	} else if len(s.Packages) > 0 {
		topics = append(topics, "local-package")
	}
	if s.Repository != nil && s.Repository.Source != "" {
		topics = append(topics, s.Repository.Source)
	}
	return topics
}

// FetchList 翻页拉全量，直到没有 next cursor 或达到分页上限。
//
// 接口路径由源自己的前缀拼出来（{base}{prefix}/servers）：官方停在 /v0，
// 子注册中心各自停在别的版本上。写死前缀等于每接一家改一次代码。
func (f *RegistryFetcher) FetchList(ctx context.Context, target mcpsource.FetchTarget) ([]mcpsource.FetchedServer, error) {
	var all []mcpsource.FetchedServer
	seen := make(map[string]bool)
	cursor := ""
	for page := 0; page < f.maxPages; page++ {
		u := fmt.Sprintf("%s%s/servers?limit=%d", target.BaseURL, target.APIPrefix, f.pageSize)
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp wireListResp
		// 有的子注册中心要密钥才给读，官方不要。带上就是了——没配的时候
		// APIKey 是空串，httpGetter 不会加那个头。
		if err := f.http.getJSON(ctx, u, target.APIKey, &resp); err != nil {
			return nil, err
		}
		for _, entry := range resp.Servers {
			s := entry.resolve()
			if s.Name == "" {
				continue
			}
			// 注册中心按版本存条目，同一个 Server 会出现多行。缓存表按
			// (source, slug) 唯一，先到的（列表按最新在前）留下。
			if seen[s.Name] {
				continue
			}
			// 下架/被删的条目不该出现在市场里。上游没给 status 时当作在架
			// ——自建注册中心不一定实现这个字段。
			if s.Status != "" && s.Status != "active" {
				continue
			}
			seen[s.Name] = true

			all = append(all, fetchedFromWire(s))
		}
		next := resp.Metadata.cursor()
		if next == "" || len(resp.Servers) == 0 {
			break
		}
		cursor = next
	}
	return all, nil
}
