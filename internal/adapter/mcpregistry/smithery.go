package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// SmitheryFetcher 拉 Smithery 的服务器目录。
//
// Smithery 没有跟着官方那套 server.json 走，自己一套：条目用
// qualifiedName（owner/name）作主键，分页是 page/pageSize + 一个
// pagination 对象，而且**要 API Key 才给读**。
//
// 最要紧的一处差异：它的 remote 是个布尔值，不是地址。也就是说列表接口只
// 告诉你"这个服务器能远程连"，不告诉你连到哪。地址按它的托管约定拼出来
// （见 deployURL），拼不出来的条目就当作不可接入落库，而不是编一个地址进
// 去让用户去踩。
type SmitheryFetcher struct {
	http     *httpGetter
	maxPages int
	pageSize int
	// deployHost 是 Smithery 托管服务器的域名。做成字段是为了测试能指到
	// httptest 上；生产走 defaultSmitheryDeployHost。
	deployHost string
}

var _ mcpsource.Fetcher = (*SmitheryFetcher)(nil)

// Smithery 托管的服务器统一在这个域名下按 qualifiedName 提供 MCP 端点。
const defaultSmitheryDeployHost = "https://server.smithery.ai"

func NewSmitheryFetcher() *SmitheryFetcher {
	return &SmitheryFetcher{
		http:       newHTTPGetter(),
		maxPages:   40,
		pageSize:   100,
		deployHost: defaultSmitheryDeployHost,
	}
}

// ── 上游 wire 结构 ────────────────────────────────────────────────────

type smitheryServer struct {
	QualifiedName string `json:"qualifiedName"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	IconURL       string `json:"iconUrl"`
	UseCount      any    `json:"useCount"` // 有时是数字有时是字符串，只用来排除空值
	Verified      bool   `json:"verified"`
	// Remote 是"能不能远程连"，不是地址。
	Remote    bool   `json:"remote"`
	CreatedAt string `json:"createdAt"`
}

type smitheryPagination struct {
	CurrentPage int `json:"currentPage"`
	PageSize    int `json:"pageSize"`
	TotalPages  int `json:"totalPages"`
	TotalCount  int `json:"totalCount"`
}

type smitheryListResp struct {
	Servers    []smitheryServer   `json:"servers"`
	Pagination smitheryPagination `json:"pagination"`
}

// deployURL 按 Smithery 的托管约定拼出 MCP 端点：
// https://server.smithery.ai/{qualifiedName}/mcp
//
// 只有 remote=true 的条目才拼——false 的那些是"下载下来自己跑"的包，平台
// 连不上，拼一个地址出来只会让用户接进去之后一探测就红。
func (f *SmitheryFetcher) deployURL(s smitheryServer) string {
	if !s.Remote || s.QualifiedName == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/mcp", strings.TrimRight(f.deployHost, "/"), strings.TrimPrefix(s.QualifiedName, "/"))
}

// FetchList 翻页拉全量。Smithery 给了 totalPages，所以停止条件比游标式的
// 更直白；但仍然受 maxPages 兜底，免得对方返回一个离谱的总页数。
func (f *SmitheryFetcher) FetchList(ctx context.Context, target mcpsource.FetchTarget) ([]mcpsource.FetchedServer, error) {
	if target.APIKey == "" {
		return nil, fmt.Errorf("这个源（Smithery）要求 API Key，先在源的设置里填一个")
	}

	var all []mcpsource.FetchedServer
	seen := make(map[string]bool)
	for page := 1; page <= f.maxPages; page++ {
		u := fmt.Sprintf("%s%s/servers?page=%d&pageSize=%d", target.BaseURL, target.APIPrefix, page, f.pageSize)
		var resp smitheryListResp
		if err := f.http.getJSON(ctx, u, target.APIKey, &resp); err != nil {
			return nil, err
		}
		if len(resp.Servers) == 0 {
			break
		}
		for _, s := range resp.Servers {
			if s.QualifiedName == "" || seen[s.QualifiedName] {
				continue
			}
			seen[s.QualifiedName] = true

			raw, _ := json.Marshal(s)
			remoteURL := f.deployURL(s)
			fs := mcpsource.FetchedServer{
				Slug:          s.QualifiedName,
				Name:          firstNonEmpty(s.DisplayName, s.QualifiedName),
				Summary:       s.Description,
				RepositoryURL: s.Homepage,
				IconURL:       s.IconURL,
				RemoteURL:     remoteURL,
				Topics:        smitheryTopics(s, remoteURL),
				Raw:           raw,
			}
			if remoteURL != "" {
				// Smithery 托管的端点都是 streamable-http。
				fs.RemoteType = "streamable-http"
			}
			if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
				fs.UpdatedAt = t.UTC()
			}
			all = append(all, fs)
		}
		// totalPages 为 0（对方没给）时靠上面的空页判断收尾。
		if resp.Pagination.TotalPages > 0 && page >= resp.Pagination.TotalPages {
			break
		}
	}
	return all, nil
}

// smitheryTopics 给条目打上页面上能筛的标签。verified 是 Smithery 自己的
// 审核标记，对我们这边的管理员是有用的一手信息——他要决定放不放行。
func smitheryTopics(s smitheryServer, remoteURL string) []string {
	topics := make([]string, 0, 2)
	if remoteURL != "" {
		topics = append(topics, "streamable-http")
	} else {
		topics = append(topics, "local-package")
	}
	if s.Verified {
		topics = append(topics, "verified")
	}
	return topics
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
