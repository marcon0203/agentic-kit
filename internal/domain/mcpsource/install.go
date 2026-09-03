package mcpsource

import (
	"context"
	"regexp"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// ServerInstaller 是安装动作对资源中心的依赖：按市场条目建一条本地 MCP
// 资源。走的是和手工接入完全相同的 Create 管线（连通性探测、凭据加密、
// ref 校验全部复用），市场安装只是替用户把地址填好、多带一份
// installed_from 标记。由 *resource.Service 实现。
type ServerInstaller interface {
	Create(ctx context.Context, ownerID int64, cmd resource.CreateCommand) (resource.Resource, error)
}

// refPattern 与资源中心一致（^[a-z][a-z0-9_-]*$）。上游的限定名形如
// io.github.owner/airtable-mcp-server，不满足这个式子，所以要转写。
var refPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var nonRefChars = regexp.MustCompile(`[^a-z0-9-]+`)

// refFromSlug 把上游限定名转成本地 ref。取最后一段（域名前缀是发布者命名
// 空间，对本地资源没有意义），其余非法字符压成连字符：
//
//	io.github.domdomegg/airtable-mcp-server → airtable-mcp-server
//	com.example/Weather.API                 → weather-api
//
// 转写后仍不合法（比如数字开头）时补一个 mcp- 前缀，而不是把请求打回去让
// 用户自己想一个名字——他要的是"装上这个"，不是给它取名。
func refFromSlug(slug string) string {
	last := slug
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		last = slug[i+1:]
	}
	ref := nonRefChars.ReplaceAllString(strings.ToLower(last), "-")
	ref = strings.Trim(ref, "-")
	if !refPattern.MatchString(ref) {
		ref = strings.Trim("mcp-"+ref, "-")
	}
	return ref
}

// Install 把市场里的一个 MCP Server 接入当前账号：按快照里的远端地址建一
// 条 mcp 资源，config 里记下 installed_from。
func (s *Service) Install(ctx context.Context, userID, id int64) (resource.Resource, error) {
	if s.installer == nil {
		return resource.Resource{}, domain.Invalid(CodeMCPSourceUpstream, "这个部署没有配置资源中心，无法安装 MCP Server")
	}
	item, err := s.repo.GetMarketServer(ctx, id)
	if err != nil {
		return resource.Resource{}, err
	}
	// 审核如果只挡列表，安装接口就是绕过它的一条路：知道 id 的人直接 POST
	// 就能把没过审的第三方地址接进来。
	if item.ReviewStatus != ReviewApproved {
		return resource.Resource{}, domain.Forbidden(CodeMarketMCPNotPassed, "这个 MCP Server 还没有通过审核，暂时不能接入")
	}
	// 只能本机起进程的条目（上游只给了 packages）本平台连不上。与其建一条
	// 一探测就红的资源，不如在这里说清楚为什么装不了。
	if !item.Installable() {
		return resource.Resource{}, domain.Invalid(CodeMarketMCPNotRemote,
			"这个 MCP Server 只提供本地运行包、没有远端地址，平台接入不了")
	}

	created, err := s.installer.Create(ctx, userID, resource.CreateCommand{
		Kind:        string(resource.KindMCP),
		Ref:         refFromSlug(item.Slug),
		DisplayName: item.Name,
		Config: resource.Config{
			"endpoint":    item.RemoteURL,
			"remote_type": item.RemoteType,
			"installed_from": map[string]any{
				"source_id":   item.SourceID,
				"source_name": item.SourceName,
				"base_url":    item.SourceBaseURL,
				"slug":        item.Slug,
				"version":     item.Version,
			},
		},
	})
	if err != nil {
		if derr, ok := domain.AsError(err); ok && derr.Code == domain.CodeResourceRefDuplicate {
			return resource.Resource{}, domain.Conflict(CodeMCPAlreadyInstalled, "已经接入过这个 MCP Server（或 ref 已被占用）")
		}
		return resource.Resource{}, err
	}
	return created, nil
}
