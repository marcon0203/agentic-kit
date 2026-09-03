// Package clawhub 实现 ClawhubFetcher：按 clawhub.ai 的公开协议拉取
// （/api/v1/skills 系列 + /api/v1/download 安装包）。它住在 adapter 层是
// 因为 domain 不允许碰 net/http（见 domain/layering_test）——协议细节全部
// 藏在这里，skillsource.Service 只面对 Fetcher 接口。
package clawhub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/skillsource"
)

// Fetcher 按 clawhub.ai 的公开协议拉取。这类站点的字段宽进严出：缺
// summary、缺 stats 都不报错，落库时留空。
type Fetcher struct {
	client *http.Client
	// maxPages 限制单次同步的分页深度，防止一个异常源把同步挂死。
	maxPages int
	pageSize int
}

var _ skillsource.Fetcher = (*Fetcher)(nil)

func NewFetcher() *Fetcher {
	return &Fetcher{
		client:   &http.Client{Timeout: 20 * time.Second},
		maxPages: 10,
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
	defer resp.Body.Close()
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

// ── 上游 wire 结构：字段全部可选，解析失败不致命 ─────────────────────

type wireStats struct {
	Stars     *int64 `json:"stars"`
	Downloads *int64 `json:"downloads"`
}

type wireLatestVersion struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	License   string `json:"license"`
}

type wireSkillItem struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Summary     string `json:"summary"`
	// 图标的几种常见写法都认。上游列表接口给不给、叫什么名字各家不一样，
	// 多认几个字段不花什么代价，认漏了就是一整片空白卡片。
	Icon          string             `json:"icon"`
	Image         string             `json:"image"`
	Logo          string             `json:"logo"`
	Topics        []string           `json:"topics"`
	Stats         wireStats          `json:"stats"`
	CreatedAt     *int64             `json:"createdAt"`
	UpdatedAt     *int64             `json:"updatedAt"`
	LatestVersion *wireLatestVersion `json:"latestVersion"`
}

type wireListResp struct {
	Items      []wireSkillItem `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

type wireOwner struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Image       string `json:"image"`
}

type wireDetailResp struct {
	Skill struct {
		wireSkillItem
		Description string `json:"description"`
	} `json:"skill"`
	Owner *wireOwner `json:"owner"`
}

type wireVersionItem struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	CreatedAt *int64 `json:"createdAt"`
}

type wireVersionsResp struct {
	Items []wireVersionItem `json:"items"`
}

func msToTime(ms *int64) time.Time {
	if ms == nil || *ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(*ms).UTC()
}

// iconOf 按优先级挑一个图标地址；都没有就返回空串，前端生成字母图标兜底。
func iconOf(it wireSkillItem) string {
	for _, v := range []string{it.Icon, it.Image, it.Logo} {
		if v != "" {
			return v
		}
	}
	return ""
}

func nameOf(it wireSkillItem) string {
	if it.DisplayName != "" {
		return it.DisplayName
	}
	return it.Slug
}

// FetchList 翻页拉全量，直到没有 nextCursor 或达到分页上限。
func (f *Fetcher) FetchList(ctx context.Context, baseURL string) ([]skillsource.FetchedSkill, error) {
	var all []skillsource.FetchedSkill
	cursor := ""
	for page := 0; page < f.maxPages; page++ {
		u := fmt.Sprintf("%s/api/v1/skills?limit=%d", baseURL, f.pageSize)
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp wireListResp
		if err := f.getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			if it.Slug == "" {
				continue
			}
			raw, _ := json.Marshal(it)
			fs := skillsource.FetchedSkill{
				Slug:      it.Slug,
				Name:      nameOf(it),
				Summary:   it.Summary,
				IconURL:   iconOf(it),
				Topics:    it.Topics,
				UpdatedAt: msToTime(it.UpdatedAt),
				Raw:       raw,
			}
			if it.Stats.Stars != nil {
				fs.Stars = *it.Stats.Stars
			}
			if it.Stats.Downloads != nil {
				fs.Downloads = *it.Stats.Downloads
			}
			if it.LatestVersion != nil {
				fs.Version = it.LatestVersion.Version
				fs.Changelog = it.LatestVersion.Changelog
				fs.License = it.LatestVersion.License
			}
			all = append(all, fs)
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" || len(resp.Items) == 0 {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

// FetchDetail 回源拉单个 Skill。上游页/{owner}/skills/{slug}就是人看的
// 详情页，直接作为外链返回。
func (f *Fetcher) FetchDetail(ctx context.Context, baseURL, slug string) (string, *skillsource.Owner, string, *json.RawMessage, error) {
	u := fmt.Sprintf("%s/api/v1/skills/%s", baseURL, url.PathEscape(slug))
	var resp wireDetailResp
	if err := f.getJSON(ctx, u, &resp); err != nil {
		return "", nil, "", nil, err
	}
	var owner *skillsource.Owner
	if resp.Owner != nil && resp.Owner.Handle != "" {
		owner = &skillsource.Owner{
			Handle:      resp.Owner.Handle,
			DisplayName: resp.Owner.DisplayName,
			Avatar:      resp.Owner.Image,
		}
	}
	upstream := ""
	if owner != nil {
		upstream = fmt.Sprintf("%s/%s/skills/%s", baseURL, url.PathEscape(owner.Handle), url.PathEscape(slug))
	}
	return resp.Skill.Description, owner, upstream, nil, nil
}

// DownloadZip 下载安装包：GET /api/v1/download?slug=&version=，zip 字节
// 原样返回，解包和安全校验在调用方。
func (f *Fetcher) DownloadZip(ctx context.Context, baseURL, slug, version string) ([]byte, error) {
	u := fmt.Sprintf("%s/api/v1/download?slug=%s", baseURL, url.QueryEscape(slug))
	if version != "" {
		u += "&version=" + url.QueryEscape(version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/zip")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上源：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("源返回 %d", resp.StatusCode)
	}
	// 解压前先限流：zip 炸弹在 extractZip 的单文件上限处再拦一道。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// FetchVersions 回源拉版本历史。
func (f *Fetcher) FetchVersions(ctx context.Context, baseURL, slug string) ([]skillsource.SkillVersion, error) {
	u := fmt.Sprintf("%s/api/v1/skills/%s/versions", baseURL, url.PathEscape(slug))
	var resp wireVersionsResp
	if err := f.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	out := make([]skillsource.SkillVersion, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, skillsource.SkillVersion{
			Version:   it.Version,
			Changelog: it.Changelog,
			CreatedAt: msToTime(it.CreatedAt),
		})
	}
	return out, nil
}
