// Package skillsource 实现系统配置 → Skill 源：管理员登记一个公开的
// Skill 市场地址（如 https://clawhub.ai/），同步后其公开 Skill 进入
// Skill 管理 → 市场视图。
//
// 同步是"缓存"而不是"导入"——上游条目只落在 market_skills 快照表里，
// 详情页按需回源拉取（作者、完整用法、版本历史列表接口才给），这既避免
// 每次同步对上游做 N+1 请求，也让详情始终是上游最新的。
package skillsource

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// 110xxx — Skill 源
const (
	CodeSkillSourceNotFound  = 110001
	CodeSkillSourceURLDup    = 110002
	CodeSkillSourceURLBad    = 110003
	CodeMarketSkillNotFound  = 110004
	CodeSkillSourceUpstream  = 110005
	CodeSkillSourceForbidden = 110006
)

// Source 是一个已登记的公开 Skill 市场。
type Source struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	BaseURL       string    `json:"base_url"`
	Status        int16     `json:"status"`
	LastSyncedAt  time.Time `json:"last_synced_at"` // zero = 从未同步
	LastSyncError string    `json:"last_sync_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	SkillCount    int64     `json:"skill_count"`
}

// MarketSkill 是同步下来的公开 Skill 快照。Raw 保留上游列表条目原文，
// 详情页把它带回前端兜底展示。
type MarketSkill struct {
	SourceID      int64           `json:"source_id"`
	SourceName    string          `json:"source_name"`
	SourceBaseURL string          `json:"source_base_url"`
	Slug          string          `json:"slug"`
	Name          string          `json:"name"`
	Summary       string          `json:"summary,omitempty"`
	Version       string          `json:"version,omitempty"`
	License       string          `json:"license,omitempty"`
	Changelog     string          `json:"changelog,omitempty"`
	Topics        []string        `json:"topics"`
	Stars         int64           `json:"stars"`
	Downloads     int64           `json:"downloads"`
	UpdatedAt     time.Time       `json:"updated_at"` // 上游的更新时间；zero = 上游没给
	Raw           json.RawMessage `json:"raw,omitempty"`
}

// Owner / SkillDetail / SkillVersion 是详情回源时从上游拿到的、列表接口
// 不提供的部分。
type Owner struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

type SkillDetail struct {
	MarketSkill
	Usage       string           `json:"usage"` // 完整用法（上游 SKILL.md 原文）
	Owner       *Owner           `json:"owner,omitempty"`
	UpstreamURL string           `json:"upstream_url,omitempty"` // 上网页面，供外链
	Versions    []SkillVersion   `json:"versions"`
	RawDetail   *json.RawMessage `json:"-"`
}

type SkillVersion struct {
	Version   string    `json:"version"`
	Changelog string    `json:"changelog,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// FetchedSkill / Fetcher：同步器面对的抽象。默认实现是 ClawhubFetcher
// （/api/v1/skills 系列端点）；将来接入协议不同的源时换一个 Fetcher 即可，
// Service 不用动。
type FetchedSkill struct {
	Slug      string
	Name      string
	Summary   string
	Version   string
	License   string
	Changelog string
	Topics    []string
	Stars     int64
	Downloads int64
	UpdatedAt time.Time
	Raw       json.RawMessage
}

type Fetcher interface {
	// FetchList 拉取一个源的全部公开 Skill（内部分页）。
	FetchList(ctx context.Context, baseURL string) ([]FetchedSkill, error)
	// FetchDetail 回源拉单个 Skill 的完整信息；网络失败不致命，调用方退回
	// 缓存快照。
	FetchDetail(ctx context.Context, baseURL, slug string) (usage string, owner *Owner, upstreamURL string, raw *json.RawMessage, err error)
	// FetchVersions 回源拉版本历史。
	FetchVersions(ctx context.Context, baseURL, slug string) ([]SkillVersion, error)
	// DownloadZip 回源下载一个 Skill 指定版本的安装包（zip 字节）。
	DownloadZip(ctx context.Context, baseURL, slug, version string) ([]byte, error)
}

// Repository 是 skill_sources / market_skills 两张表的持久化抽象。
type Repository interface {
	Create(ctx context.Context, name, baseURL string) (Source, error)
	List(ctx context.Context) ([]Source, error)
	Get(ctx context.Context, id int64) (Source, error)
	GetByURL(ctx context.Context, baseURL string) (Source, error)
	Delete(ctx context.Context, id int64) error
	MarkSynced(ctx context.Context, id int64) error
	MarkSyncError(ctx context.Context, id int64, msg string) error
	ReplaceSkills(ctx context.Context, sourceID int64, skills []FetchedSkill) error
	ListMarketSkills(ctx context.Context) ([]MarketSkill, error)
	GetMarketSkill(ctx context.Context, sourceID int64, slug string) (MarketSkill, error)
}

// AdminDirectory 与 modelcatalog 的同名接口一致：管理面权限判定收在
// Service 里，403 的理由只有一个出处。
type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

// Service 是 Skill 源的应用服务。
type Service struct {
	repo      Repository
	admins    AdminDirectory
	fetch     Fetcher
	installer SkillInstaller
}

func NewService(repo Repository, admins AdminDirectory, fetch Fetcher, installer SkillInstaller) *Service {
	return &Service{
		repo:      repo,
		admins:    admins,
		fetch:     fetch,
		installer: installer,
	}
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	ok, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.Forbidden(CodeSkillSourceForbidden, "只有管理员可以管理 Skill 源")
	}
	return nil
}

// normalizeBaseURL 统一登记地址：去掉尾斜杠、必须 http(s)。返回值即存储
// 与判重用的规范形。
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", domain.Invalid(CodeSkillSourceURLBad, "源地址必须是完整的 http(s) URL")
	}
	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", domain.Invalid(CodeSkillSourceURLBad, "源地址只要站点根地址，不要带路径")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Create 登记一个新源（管理员）。
func (s *Service) Create(ctx context.Context, userID int64, name, baseURL string) (Source, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Source{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Source{}, domain.Invalid(CodeSkillSourceURLBad, "源名称不能为空")
	}
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return Source{}, err
	}
	if _, err := s.repo.GetByURL(ctx, normalized); err == nil {
		return Source{}, domain.Conflict(CodeSkillSourceURLDup, "这个源已经登记过了")
	}
	return s.repo.Create(ctx, name, normalized)
}

// List 列出全部源（管理员）。
func (s *Service) List(ctx context.Context, userID int64) ([]Source, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

// Delete 删除一个源；级联清掉它的缓存条目（FK ON DELETE CASCADE）。
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// Sync 同步一个源：拉全量列表、整体替换缓存。失败写进 last_sync_error，
// 下次设置页直接可见，不静默。
func (s *Service) Sync(ctx context.Context, userID, id int64) (Source, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Source{}, err
	}
	src, err := s.repo.Get(ctx, id)
	if err != nil {
		return Source{}, err
	}
	skills, err := s.fetch.FetchList(ctx, src.BaseURL)
	if err != nil {
		_ = s.repo.MarkSyncError(ctx, id, err.Error())
		updated, getErr := s.repo.Get(ctx, id)
		if getErr == nil {
			return updated, err
		}
		return Source{}, err
	}
	if err := s.repo.ReplaceSkills(ctx, id, skills); err != nil {
		_ = s.repo.MarkSyncError(ctx, id, err.Error())
		return Source{}, err
	}
	if err := s.repo.MarkSynced(ctx, id); err != nil {
		return Source{}, err
	}
	return s.repo.Get(ctx, id)
}

// ListMarketSkills 供 Skill 管理 → 市场视图：所有启用源的缓存条目。
// 任何登录用户可看——公开源本来就是公开内容。
func (s *Service) ListMarketSkills(ctx context.Context) ([]MarketSkill, error) {
	return s.repo.ListMarketSkills(ctx)
}

// GetMarketSkill 取一个 Skill 的完整详情：缓存快照打底，再回源补用法、
// 作者和版本历史。回源失败不报错（上游可能临时不可达），页面退回缓存字段。
func (s *Service) GetMarketSkill(ctx context.Context, sourceID int64, slug string) (SkillDetail, error) {
	cached, err := s.repo.GetMarketSkill(ctx, sourceID, slug)
	if err != nil {
		return SkillDetail{}, err
	}
	detail := SkillDetail{MarketSkill: cached}

	if usage, owner, upstream, raw, ferr := s.fetch.FetchDetail(ctx, cached.SourceBaseURL, slug); ferr == nil {
		if usage != "" {
			detail.Usage = usage
		}
		detail.Owner = owner
		detail.UpstreamURL = upstream
		detail.RawDetail = raw
	}
	if versions, verr := s.fetch.FetchVersions(ctx, cached.SourceBaseURL, slug); verr == nil && len(versions) > 0 {
		detail.Versions = versions
	}
	return detail, nil
}
