// Package mcpregistry 实现按官方 MCP Registry 协议拉取的 Fetcher
// （GET /v0/servers，游标翻页）。它住在 adapter 层是因为 domain 不允许碰
// net/http（见 domain/layering_test）——协议细节全部藏在这里，
// mcpsource.Service 只面对 Fetcher 接口。
package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// Fetcher 按 registry.modelcontextprotocol.io 的公开协议拉取。字段宽进严
// 出：缺 description、缺 repository 都不报错，落库时留空。
type Fetcher struct {
	client *http.Client
	// maxPages 限制单次同步的分页深度，防止一个异常源把同步挂死。
	maxPages int
	pageSize int
}

var _ mcpsource.Fetcher = (*Fetcher)(nil)

func NewFetcher() *Fetcher {
	return &Fetcher{
		client:   &http.Client{Timeout: 20 * time.Second},
		maxPages: 40,
		pageSize: 100,
	}
}

func (f *Fetcher) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("连不上源：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("源返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("源的响应不是合法 JSON：%w", err)
	}
	return nil
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

type wireServer struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Version     string          `json:"version"`
	Repository  *wireRepository `json:"repository"`
	Remotes     []wireRemote    `json:"remotes"`
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
func (f *Fetcher) FetchList(ctx context.Context, baseURL string) ([]mcpsource.FetchedServer, error) {
	var all []mcpsource.FetchedServer
	seen := make(map[string]bool)
	cursor := ""
	for page := 0; page < f.maxPages; page++ {
		u := fmt.Sprintf("%s/v0/servers?limit=%d", baseURL, f.pageSize)
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp wireListResp
		if err := f.getJSON(ctx, u, &resp); err != nil {
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

			remoteURL, remoteType := pickRemote(s.Remotes)
			raw, _ := json.Marshal(s)
			fs := mcpsource.FetchedServer{
				Slug:       s.Name,
				Name:       s.Name,
				Summary:    s.Description,
				Version:    s.Version,
				RemoteURL:  remoteURL,
				RemoteType: remoteType,
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
			all = append(all, fs)
		}
		next := resp.Metadata.cursor()
		if next == "" || len(resp.Servers) == 0 {
			break
		}
		cursor = next
	}
	return all, nil
}
