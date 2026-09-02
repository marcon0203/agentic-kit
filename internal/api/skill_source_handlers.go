package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/skillsource"
)

// SkillSourceHandlers is 系统配置 → Skill 源 + Skill 管理 → 市场视图的
// HTTP transport。源管理（增删列、手动同步）只有管理员可用；市场视图任
// 何登录用户可看——公开源本来就是公开内容。权限判定在 skillsource.Service。
type SkillSourceHandlers struct {
	svc *skillsource.Service
}

func NewSkillSourceHandlers(svc *skillsource.Service) *SkillSourceHandlers {
	return &SkillSourceHandlers{svc: svc}
}

type skillSourceDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	Status        int16  `json:"status"`
	LastSyncedAt  string `json:"last_synced_at,omitempty"` // RFC3339；空 = 从未同步
	LastSyncError string `json:"last_sync_error,omitempty"`
	SkillCount    int64  `json:"skill_count"`
}

func toSkillSourceDTO(s skillsource.Source) skillSourceDTO {
	dto := skillSourceDTO{
		ID: s.ID, Name: s.Name, BaseURL: s.BaseURL, Status: s.Status,
		LastSyncError: s.LastSyncError, SkillCount: s.SkillCount,
	}
	if !s.LastSyncedAt.IsZero() {
		dto.LastSyncedAt = s.LastSyncedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

type marketSkillDTO struct {
	SourceID      int64    `json:"source_id"`
	SourceName    string   `json:"source_name"`
	SourceBaseURL string   `json:"source_base_url"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Summary       string   `json:"summary,omitempty"`
	Version       string   `json:"version,omitempty"`
	License       string   `json:"license,omitempty"`
	Topics        []string `json:"topics"`
	Stars         int64    `json:"stars"`
	Downloads     int64    `json:"downloads"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	// 审核字段：用户侧列表拿到的永远是 approved，这几个字段是给审核台看的。
	ReviewStatus string `json:"review_status"`
	ReviewNote   string `json:"review_note,omitempty"`
	ReviewedAt   string `json:"reviewed_at,omitempty"`
	SyncedAt     string `json:"synced_at,omitempty"`
}

func toMarketSkillDTO(m skillsource.MarketSkill) marketSkillDTO {
	dto := marketSkillDTO{
		SourceID: m.SourceID, SourceName: m.SourceName, SourceBaseURL: m.SourceBaseURL,
		Slug: m.Slug, Name: m.Name, Summary: m.Summary, Version: m.Version,
		License: m.License,
		Topics: m.Topics, Stars: m.Stars, Downloads: m.Downloads,
		ReviewStatus: string(m.ReviewStatus), ReviewNote: m.ReviewNote,
	}
	if !m.ReviewedAt.IsZero() {
		dto.ReviewedAt = m.ReviewedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if !m.SyncedAt.IsZero() {
		dto.SyncedAt = m.SyncedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if !m.UpdatedAt.IsZero() {
		dto.UpdatedAt = m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

type marketSkillDetailDTO struct {
	marketSkillDTO
	Usage       string               `json:"usage,omitempty"`
	Owner       *skillsource.Owner   `json:"owner,omitempty"`
	UpstreamURL string               `json:"upstream_url,omitempty"`
	Versions    []marketVersionDTO  `json:"versions"`
}

type marketVersionDTO struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// List handles GET /skill-sources (admin).
func (h *SkillSourceHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sources, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]skillSourceDTO, 0, len(sources))
	for _, s := range sources {
		items = append(items, toSkillSourceDTO(s))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// Create handles POST /skill-sources (admin).
func (h *SkillSourceHandlers) Create(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, r, http.StatusCreated, toSkillSourceDTO(src))
}

// Sync handles POST /skill-sources/{id}/sync (admin).
func (h *SkillSourceHandlers) Sync(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, r, http.StatusOK, toSkillSourceDTO(src))
}

// Delete handles DELETE /skill-sources/{id} (admin).
func (h *SkillSourceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
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

// MarketInstall handles POST /skill-market/{source_id}/{slug}/install：
// 下载上游安装包并作为当前账号的 Skill 资源落库（config 带 installed_from）。
func (h *SkillSourceHandlers) MarketInstall(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sourceID, ok := parseIDParam(w, r, "source_id")
	if !ok {
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid slug")
		return
	}
	created, err := h.svc.Install(r.Context(), userID, sourceID, slug)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toResourceDTO(created))
}

// ListMarket handles GET /skill-market：Skill 管理 → 市场视图的卡片列表。
func (h *SkillSourceHandlers) ListMarket(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	skills, err := h.svc.ListMarketSkills(r.Context())
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]marketSkillDTO, 0, len(skills))
	for _, m := range skills {
		items = append(items, toMarketSkillDTO(m))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "has_more": false})
}

// ListForReview handles GET /skill-sources/skills：审核台的列表，同步进来
// 的全部条目（可按 review_status / source_id 筛）。管理员限定。
func (h *SkillSourceHandlers) ListForReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	status := skillsource.ReviewStatus(r.URL.Query().Get("review_status"))
	var sourceID int64
	if raw := r.URL.Query().Get("source_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid source_id")
			return
		}
		sourceID = parsed
	}

	skills, err := h.svc.ListForReview(r.Context(), userID, status, sourceID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	counts, err := h.svc.ReviewCounts(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	items := make([]marketSkillDTO, 0, len(skills))
	for _, m := range skills {
		items = append(items, toMarketSkillDTO(m))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items,
		"counts": map[string]int64{
			"pending":  counts[skillsource.ReviewPending],
			"approved": counts[skillsource.ReviewApproved],
			"rejected": counts[skillsource.ReviewRejected],
		},
	})
}

type reviewSkillsRequest struct {
	Items []struct {
		SourceID int64  `json:"source_id"`
		Slug     string `json:"slug"`
	} `json:"items"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ReviewSkills handles POST /skill-sources/skills/review：给一批同步条目
// 下同一个审核结论。批量是主路径——一个公开源同步下来动辄成百上千条，逐
// 条点审不完。
func (h *SkillSourceHandlers) ReviewSkills(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req reviewSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	items := make([]skillsource.ReviewItem, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, skillsource.ReviewItem{SourceID: it.SourceID, Slug: it.Slug})
	}
	if err := h.svc.Review(r.Context(), userID, items, skillsource.ReviewStatus(req.Status), req.Note); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"reviewed": len(items)})
}

// MarketDetail handles GET /skill-market/{source_id}/{slug}。
func (h *SkillSourceHandlers) MarketDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sourceID, ok := parseIDParam(w, r, "source_id")
	if !ok {
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid slug")
		return
	}
	detail, err := h.svc.GetMarketSkill(r.Context(), userID, sourceID, slug)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	dto := marketSkillDetailDTO{
		marketSkillDTO: toMarketSkillDTO(detail.MarketSkill),
		Usage:          detail.Usage,
		Owner:          detail.Owner,
		UpstreamURL:    detail.UpstreamURL,
	}
	dto.Versions = make([]marketVersionDTO, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		vd := marketVersionDTO{Version: v.Version, Changelog: v.Changelog}
		if !v.CreatedAt.IsZero() {
			vd.CreatedAt = v.CreatedAt.Format("2006-01-02")
		}
		dto.Versions = append(dto.Versions, vd)
	}
	writeJSON(w, r, http.StatusOK, dto)
}
