// Package mcpsource 实现系统配置 → MCP 源：管理员登记一个公开的 MCP 注册
// 中心（如官方 https://registry.modelcontextprotocol.io），同步后其公开
// Server 进入 MCP 管理 → 市场视图。
//
// 和 skillsource 是同一套流程（登记 → 同步 → 审核 → 安装），差别只在两处：
//
//   - 同步是"缓存"而不是"导入"：上游条目只落在 market_mcp_servers 快照表
//     里，安装时才按快照建一条本地 MCP 资源。
//   - 条目对外用行 id 寻址而不是 slug：MCP 的限定名形如
//     io.github.owner/server，带点和斜杠，做不了 URL 路径参数。
package mcpsource

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// 111xxx — MCP 源（110xxx 是 Skill 源）
const (
	CodeMCPSourceNotFound   = 111001
	CodeMCPSourceURLDup     = 111002
	CodeMCPSourceURLBad     = 111003
	CodeMarketMCPNotFound   = 111004
	CodeMCPSourceUpstream   = 111005
	CodeMCPSourceForbidden  = 111006
	CodeMarketMCPNotPassed  = 111007
	CodeReviewStatusInvalid = 111008
	CodeMarketMCPNotRemote  = 111009
	CodeMCPAlreadyInstalled = 111010
)

// ReviewStatus 是一条同步条目的本地审核结论。同步进来的条目默认 pending，
// 只有 approved 才进用户侧的市场视图——一个公开注册中心里谁都能发布，默认
// 放行等于把任意第三方地址直接摆进用户的可接入列表。
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"
	ReviewApproved ReviewStatus = "approved"
	ReviewRejected ReviewStatus = "rejected"
)

func (r ReviewStatus) valid() bool {
	return r == ReviewPending || r == ReviewApproved || r == ReviewRejected
}

// Source 是一个已登记的公开 MCP 注册中心。
type Source struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	BaseURL       string    `json:"base_url"`
	Status        int16     `json:"status"`
	LastSyncedAt  time.Time `json:"last_synced_at"` // zero = 从未同步
	LastSyncError string    `json:"last_sync_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ServerCount   int64     `json:"server_count"`
}

// MarketServer 是同步下来的公开 MCP Server 快照。
type MarketServer struct {
	ID            int64  `json:"id"`
	SourceID      int64  `json:"source_id"`
	SourceName    string `json:"source_name"`
	SourceBaseURL string `json:"source_base_url"`
	Slug          string `json:"slug"` // 上游限定名，如 io.github.owner/server
	Name          string `json:"name"`
	Summary       string `json:"summary,omitempty"`
	Version       string `json:"version,omitempty"`
	License       string `json:"license,omitempty"`
	RepositoryURL string `json:"repository_url,omitempty"`
	// RemoteURL 空 = 这个 Server 只能在本机起进程（上游只给了 packages），
	// 平台装不了它。页面据此把安装按钮置灰并说明原因。
	RemoteURL  string          `json:"remote_url,omitempty"`
	RemoteType string          `json:"remote_type,omitempty"` // streamable-http | sse
	Topics     []string        `json:"topics"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Raw        json.RawMessage `json:"raw,omitempty"`

	ReviewStatus ReviewStatus `json:"review_status"`
	ReviewNote   string       `json:"review_note,omitempty"`
	ReviewedAt   time.Time    `json:"reviewed_at"`
	SyncedAt     time.Time    `json:"synced_at"`
}

// Installable 报告这条目能不能一键接入：本平台连的是远端 MCP 地址，只能
// 本地起进程的条目装不了。
func (m MarketServer) Installable() bool { return m.RemoteURL != "" }

// FetchedServer 是同步器交给仓储的一条。Fetcher 的实现负责把各家注册中心
// 的 wire 结构收敛到这个形状。
type FetchedServer struct {
	Slug          string
	Name          string
	Summary       string
	Version       string
	License       string
	RepositoryURL string
	RemoteURL     string
	RemoteType    string
	Topics        []string
	UpdatedAt     time.Time
	Raw           json.RawMessage
}

// Fetcher 是同步器面对的抽象。默认实现按官方 MCP Registry 的 /v0/servers
// 协议拉取；将来接入协议不同的源时换一个 Fetcher，Service 不用动。
type Fetcher interface {
	FetchList(ctx context.Context, baseURL string) ([]FetchedServer, error)
}

// Repository 是 mcp_sources / market_mcp_servers 两张表的持久化抽象。
type Repository interface {
	Create(ctx context.Context, name, baseURL string) (Source, error)
	List(ctx context.Context) ([]Source, error)
	Get(ctx context.Context, id int64) (Source, error)
	GetByURL(ctx context.Context, baseURL string) (Source, error)
	Delete(ctx context.Context, id int64) error
	MarkSynced(ctx context.Context, id int64) error
	MarkSyncError(ctx context.Context, id int64, msg string) error
	ReplaceServers(ctx context.Context, sourceID int64, servers []FetchedServer) error
	ListMarketServers(ctx context.Context) ([]MarketServer, error)
	GetMarketServer(ctx context.Context, id int64) (MarketServer, error)
	// ListMarketServersForReview 不做审核状态过滤（query 里各字段为零值时该
	// 维度不筛）。分页和搜索都在库里做——一个公开注册中心动辄上千条，全量
	// 捞回来再由上层切片等于白分页。
	ListMarketServersForReview(ctx context.Context, q ReviewQuery) ([]MarketServer, error)
	CountMarketServersForReview(ctx context.Context, q ReviewQuery) (int64, error)
	CountByReviewStatus(ctx context.Context, sourceID int64) (map[ReviewStatus]int64, error)
	SetReview(ctx context.Context, id int64, status ReviewStatus, note string, reviewerID int64) error
}

// AdminDirectory 与 skillsource 的同名接口一致：管理面权限判定收在 Service
// 里，403 的理由只有一个出处。
type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

// Service 是 MCP 源的应用服务。
type Service struct {
	repo      Repository
	admins    AdminDirectory
	fetch     Fetcher
	installer ServerInstaller
}

func NewService(repo Repository, admins AdminDirectory, fetch Fetcher, installer ServerInstaller) *Service {
	return &Service{repo: repo, admins: admins, fetch: fetch, installer: installer}
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	ok, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.Forbidden(CodeMCPSourceForbidden, "只有管理员可以管理 MCP 源")
	}
	return nil
}

// normalizeBaseURL 统一登记地址：去掉尾斜杠、必须 http(s)。返回值即存储与
// 判重用的规范形。
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", domain.Invalid(CodeMCPSourceURLBad, "源地址必须是完整的 http(s) URL")
	}
	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", domain.Invalid(CodeMCPSourceURLBad, "源地址只要站点根地址，不要带路径")
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
		return Source{}, domain.Invalid(CodeMCPSourceURLBad, "源名称不能为空")
	}
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return Source{}, err
	}
	if _, err := s.repo.GetByURL(ctx, normalized); err == nil {
		return Source{}, domain.Conflict(CodeMCPSourceURLDup, "这个源已经登记过了")
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

// Delete 删除一个源；级联清掉它的缓存条目（FK ON DELETE CASCADE）。已经
// 安装成本地资源的 MCP Server 不受影响——那是用户自己的资源，不是缓存。
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
	servers, err := s.fetch.FetchList(ctx, src.BaseURL)
	if err != nil {
		_ = s.repo.MarkSyncError(ctx, id, err.Error())
		if updated, getErr := s.repo.Get(ctx, id); getErr == nil {
			return updated, err
		}
		return Source{}, err
	}
	if err := s.repo.ReplaceServers(ctx, id, servers); err != nil {
		_ = s.repo.MarkSyncError(ctx, id, err.Error())
		return Source{}, err
	}
	if err := s.repo.MarkSynced(ctx, id); err != nil {
		return Source{}, err
	}
	return s.repo.Get(ctx, id)
}

// ListMarketServers 供 MCP 管理 → 市场视图：所有启用源里过审的缓存条目。
// 任何登录用户可看——公开注册中心本来就是公开内容。
func (s *Service) ListMarketServers(ctx context.Context) ([]MarketServer, error) {
	return s.repo.ListMarketServers(ctx)
}

// ReviewQuery 是审核台一次查询的全部条件。Status/SourceID/Search 为零值表
// 示该维度不筛；Limit <= 0 时由 ListForReview 补默认值。
type ReviewQuery struct {
	Status   ReviewStatus
	SourceID int64
	Search   string
	Limit    int
	Offset   int
}

// 审核台每页条数：默认 15（一屏能审完的量），上限 100 挡住
// ?page_size=99999 这种把分页绕过去的调用。
const (
	ReviewPageSizeDefault = 15
	ReviewPageSizeMax     = 100
)

func (q ReviewQuery) normalize() ReviewQuery {
	q.Search = strings.TrimSpace(q.Search)
	if q.Limit <= 0 {
		q.Limit = ReviewPageSizeDefault
	}
	if q.Limit > ReviewPageSizeMax {
		q.Limit = ReviewPageSizeMax
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q
}

// ReviewPage 是审核台的一页：当页条目 + 该筛选条件下的总数（前端据此算总
// 页数），外加各审核状态的条目数（顶部那几个筛选按钮上的数字）。
type ReviewPage struct {
	Items  []MarketServer
	Total  int64
	Counts map[ReviewStatus]int64
}

// ListForReview 是审核台的一页：同步进来的条目，不管审核状态、也不管源是
// 否停用。
func (s *Service) ListForReview(ctx context.Context, userID int64, q ReviewQuery) (ReviewPage, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return ReviewPage{}, err
	}
	if q.Status != "" && !q.Status.valid() {
		return ReviewPage{}, domain.Invalid(CodeReviewStatusInvalid, "unknown review status").
			WithDetails(domain.FieldError{Field: "review_status", Reason: "must be pending, approved or rejected"})
	}
	q = q.normalize()

	items, err := s.repo.ListMarketServersForReview(ctx, q)
	if err != nil {
		return ReviewPage{}, err
	}
	total, err := s.repo.CountMarketServersForReview(ctx, q)
	if err != nil {
		return ReviewPage{}, err
	}
	// 顶部计数按源统计：审核台是从某个源点进来的，统计全库的话上面写着
	// "待审核 800"、下面列表只有 12 条，对不上。
	counts, err := s.repo.CountByReviewStatus(ctx, q.SourceID)
	if err != nil {
		return ReviewPage{}, err
	}
	return ReviewPage{Items: items, Total: total, Counts: counts}, nil
}

// Review 批量给条目下审核结论。整批用同一个结论——"通过这一批"和"驳回这
// 一批"是两个动作，混在一次请求里没有意义。
func (s *Service) Review(ctx context.Context, userID int64, ids []int64, status ReviewStatus, note string) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if !status.valid() {
		return domain.Invalid(CodeReviewStatusInvalid, "unknown review status").
			WithDetails(domain.FieldError{Field: "status", Reason: "must be pending, approved or rejected"})
	}
	if len(ids) == 0 {
		return domain.Invalid(domain.CodeValidationFailed, "invalid request").
			WithDetails(domain.FieldError{Field: "ids", Reason: "required"})
	}
	for _, id := range ids {
		if err := s.repo.SetReview(ctx, id, status, note, userID); err != nil {
			return err
		}
	}
	return nil
}

// GetMarketServer 取一条市场条目的详情。未过审的条目只有管理员看得到——
// 否则审核就只挡住了列表，详情页仍然是一条绕过去的路。
func (s *Service) GetMarketServer(ctx context.Context, userID, id int64) (MarketServer, error) {
	item, err := s.repo.GetMarketServer(ctx, id)
	if err != nil {
		return MarketServer{}, err
	}
	if item.ReviewStatus != ReviewApproved {
		if adminErr := s.requireAdmin(ctx, userID); adminErr != nil {
			return MarketServer{}, domain.NotFound(CodeMarketMCPNotFound, "mcp server not found")
		}
	}
	return item, nil
}
