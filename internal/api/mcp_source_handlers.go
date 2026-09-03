package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
)

// rfc3339Layout 是 DTO 里所有时间字段的格式。时间在响应里一律是字符串而不
// 是 epoch：前端直接展示，不用再猜单位。
const rfc3339Layout = "2006-01-02T15:04:05Z07:00"

// MCPSourceHandlers is 系统配置 → MCP 源 + MCP 管理 → 市场视图的 HTTP
// transport。源管理（增删列、手动同步、审核）只有管理员可用；市场视图任
// 何登录用户可看——公开注册中心本来就是公开内容。权限判定在
// mcpsource.Service。
type MCPSourceHandlers struct {
	svc *mcpsource.Service
}

func NewMCPSourceHandlers(svc *mcpsource.Service) *MCPSourceHandlers {
	return &MCPSourceHandlers{svc: svc}
}

type mcpSourceDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	Status        int16  `json:"status"`
	LastSyncedAt  string `json:"last_synced_at,omitempty"` // RFC3339；空 = 从未同步
	LastSyncError string `json:"last_sync_error,omitempty"`
	ServerCount   int64  `json:"server_count"`
}

func toMCPSourceDTO(s mcpsource.Source) mcpSourceDTO {
	dto := mcpSourceDTO{
		ID: s.ID, Name: s.Name, BaseURL: s.BaseURL, Status: s.Status,
		LastSyncError: s.LastSyncError, ServerCount: s.ServerCount,
	}
	if !s.LastSyncedAt.IsZero() {
		dto.LastSyncedAt = s.LastSyncedAt.Format(rfc3339Layout)
	}
	return dto
}

type marketMCPServerDTO struct {
	ID            int64    `json:"id"`
	SourceID      int64    `json:"source_id"`
	SourceName    string   `json:"source_name"`
	SourceBaseURL string   `json:"source_base_url"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Summary       string   `json:"summary,omitempty"`
	Version       string   `json:"version,omitempty"`
	License       string   `json:"license,omitempty"`
	RepositoryURL string   `json:"repository_url,omitempty"`
	RemoteURL     string   `json:"remote_url,omitempty"`
	RemoteType    string   `json:"remote_type,omitempty"`
	Topics        []string `json:"topics"`
	// Installable 是给页面用的结论，省得前端再抄一遍"有 remote_url 才装得
	// 上"的判断——那条规则的出处应该只有领域层一处。
	Installable  bool   `json:"installable"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	ReviewStatus string `json:"review_status"`
	ReviewNote   string `json:"review_note,omitempty"`
	ReviewedAt   string `json:"reviewed_at,omitempty"`
	SyncedAt     string `json:"synced_at,omitempty"`
}

func toMarketMCPServerDTO(m mcpsource.MarketServer) marketMCPServerDTO {
	dto := marketMCPServerDTO{
		ID: m.ID, SourceID: m.SourceID, SourceName: m.SourceName, SourceBaseURL: m.SourceBaseURL,
		Slug: m.Slug, Name: m.Name, Summary: m.Summary, Version: m.Version, License: m.License,
		RepositoryURL: m.RepositoryURL, RemoteURL: m.RemoteURL, RemoteType: m.RemoteType,
		Topics: m.Topics, Installable: m.Installable(),
		ReviewStatus: string(m.ReviewStatus), ReviewNote: m.ReviewNote,
	}
	if dto.Topics == nil {
		dto.Topics = []string{}
	}
	if !m.UpdatedAt.IsZero() {
		dto.UpdatedAt = m.UpdatedAt.Format(rfc3339Layout)
	}
	if !m.ReviewedAt.IsZero() {
		dto.ReviewedAt = m.ReviewedAt.Format(rfc3339Layout)
	}
	if !m.SyncedAt.IsZero() {
		dto.SyncedAt = m.SyncedAt.Format(rfc3339Layout)
	}
	return dto
}

// List handles GET /mcp-sources (admin).
func (h *MCPSourceHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sources, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]mcpSourceDTO, 0, len(sources))
	for _, s := range sources {
		items = append(items, toMCPSourceDTO(s))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// Create handles POST /mcp-sources (admin).
func (h *MCPSourceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "请求体不是合法 JSON")
		return
	}
	src, err := h.svc.Create(r.Context(), userID, req.Name, req.BaseURL)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toMCPSourceDTO(src))
}

// Sync handles POST /mcp-sources/{id}/sync (admin).
func (h *MCPSourceHandlers) Sync(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	src, err := h.svc.Sync(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toMCPSourceDTO(src))
}

// Delete handles DELETE /mcp-sources/{id} (admin).
func (h *MCPSourceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMarket handles GET /mcp-market：MCP 管理 → 市场视图的卡片列表。
func (h *MCPSourceHandlers) ListMarket(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	servers, err := h.svc.ListMarketServers(r.Context())
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]marketMCPServerDTO, 0, len(servers))
	for _, m := range servers {
		items = append(items, toMarketMCPServerDTO(m))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "has_more": false})
}

// MarketDetail handles GET /mcp-market/{id}。条目按行 id 寻址：上游的限定
// 名形如 io.github.owner/server，带斜杠，做不了路径参数。
func (h *MCPSourceHandlers) MarketDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.svc.GetMarketServer(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toMarketMCPServerDTO(item))
}

// MarketInstall handles POST /mcp-market/{id}/install：按市场条目建一条当
// 前账号的 MCP 资源（config 带 installed_from）。
func (h *MCPSourceHandlers) MarketInstall(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	created, err := h.svc.Install(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toResourceDTO(created))
}

// ListForReview handles GET /mcp-sources/servers：审核台的一页，同步进来
// 的条目（可按 review_status / source_id / q 筛，page + page_size 分页）。
// 管理员限定。
func (h *MCPSourceHandlers) ListForReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	query := mcpsource.ReviewQuery{
		Status: mcpsource.ReviewStatus(q.Get("review_status")),
		Search: q.Get("q"),
	}
	if raw := q.Get("source_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid source_id")
			return
		}
		query.SourceID = parsed
	}
	page, pageSize, ok := parseReviewPaging(w, r)
	if !ok {
		return
	}
	query.Limit = pageSize
	query.Offset = (page - 1) * pageSize

	result, err := h.svc.ListForReview(r.Context(), userID, query)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]marketMCPServerDTO, 0, len(result.Items))
	for _, m := range result.Items {
		items = append(items, toMarketMCPServerDTO(m))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"items":     items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
		"has_more":  int64(query.Offset+len(items)) < result.Total,
		"counts": map[string]int64{
			"pending":  result.Counts[mcpsource.ReviewPending],
			"approved": result.Counts[mcpsource.ReviewApproved],
			"rejected": result.Counts[mcpsource.ReviewRejected],
		},
	})
}

// ReviewServers handles POST /mcp-sources/servers/review：给一批同步条目
// 下同一个审核结论。批量是主路径——一个公开注册中心同步下来动辄上千条，
// 逐条点审不完。
func (h *MCPSourceHandlers) ReviewServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs    []int64 `json:"ids"`
		Status string  `json:"status"`
		Note   string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	if err := h.svc.Review(r.Context(), userID, req.IDs, mcpsource.ReviewStatus(req.Status), req.Note); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"reviewed": len(req.IDs)})
}
