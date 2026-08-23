package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// OperationHandlers implements spec-18's 运营中心: browsing the requesting
// user's own human-gate audit trail, submitting a report against a
// marketplace listing, and — for is_admin users only — working the report
// queue and taking a reported listing down.
//
// This is deliberately thin: aggregation over other services' own tables
// (bundle_runs for the run list/cost report, which /runs and /usage/me
// already serve), not a new business-logic owner. Audit log and the report
// queue are the two pieces of state that didn't have an endpoint yet.
type OperationHandlers struct {
	Queries store.Querier
}

func NewOperationHandlers(q store.Querier) *OperationHandlers {
	return &OperationHandlers{Queries: q}
}

func startCursorDesc(raw string) int64 {
	if raw == "" {
		return math.MaxInt64
	}
	return decodeCursor(raw)
}

// ── Audit log ────────────────────────────────────────────────────────

type auditLogDTO struct {
	ID         string          `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Detail     json.RawMessage `json:"detail"`
	CreatedAt  time.Time       `json:"created_at"`
}

func toAuditLogDTO(row store.AuditLog) auditLogDTO {
	detail := row.Detail
	if detail == nil {
		detail = json.RawMessage("null")
	}
	return auditLogDTO{
		ID: strconv.FormatInt(row.ID, 10), Action: row.Action, TargetType: row.TargetType,
		TargetID: row.TargetID, Detail: detail, CreatedAt: row.CreatedAt.Time,
	}
}

// ListMyAuditLogs handles GET /audit-logs — the requesting user's own
// actions (human gate approvals/rejections today; append-only, DB-trigger
// enforced, see migration 0010).
func (h *OperationHandlers) ListMyAuditLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := startCursorDesc(r.URL.Query().Get("cursor"))

	rows, err := h.Queries.ListAuditLogsForActorPage(r.Context(), store.ListAuditLogsForActorPageParams{
		ActorUserID: pgtype.Int8{Valid: true, Int64: userID}, ID: before, Limit: int32(limit + 1),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]auditLogDTO, len(rows))
	for i, row := range rows {
		items[i] = toAuditLogDTO(row)
	}
	var nextCursor *string
	if hasMore {
		c := encodeCursor(rows[len(rows)-1].ID)
		nextCursor = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

// ── Reports (submit) ─────────────────────────────────────────────────

type createReportRequest struct {
	Reason string `json:"reason"`
}

type reportDTO struct {
	ID              string     `json:"id"`
	ListingRef      string     `json:"listing_ref"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	Resolution      *string    `json:"resolution"`
	SubscriberCount int32      `json:"subscriber_count"`
	CreatedAt       time.Time  `json:"created_at"`
	ResolvedAt      *time.Time `json:"resolved_at"`
}

// SubmitReport handles POST /marketplace/listings/{ref}/report. Any
// authenticated user may report a listing; the report lands in the admin
// moderation queue (GET /moderation/reports).
func (h *OperationHandlers) SubmitReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	ref := chi.URLParam(r, "ref")

	var req createReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Reason == "" {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed",
			[]FieldError{{Field: "reason", Reason: "required"}})
		return
	}

	listing, err := h.Queries.GetListingByListingRefLatestPublished(r.Context(), ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrListingNotFound, "listing 不存在")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	report, err := h.Queries.CreateReport(r.Context(), store.CreateReportParams{
		ListingID: listing.ID, ReporterUserID: userID, Reason: req.Reason,
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusCreated, reportToDTO(report, listing.ListingRef, listing.SubscriberCount))
}

func reportToDTO(rep store.Report, listingRef string, subscriberCount int32) reportDTO {
	dto := reportDTO{
		ID: strconv.FormatInt(rep.ID, 10), ListingRef: listingRef, Reason: rep.Reason,
		Status: rep.Status, SubscriberCount: subscriberCount, CreatedAt: rep.CreatedAt.Time,
	}
	if rep.Resolution.Valid {
		dto.Resolution = &rep.Resolution.String
	}
	if rep.ResolvedAt.Valid {
		dto.ResolvedAt = &rep.ResolvedAt.Time
	}
	return dto
}

// ── Moderation (admin only) ─────────────────────────────────────────

// requireAdmin returns 403 + 20003 unless the requesting user has
// is_admin set. Mirrors RequireOwner's shape (auth_middleware.go).
func (h *OperationHandlers) requireAdmin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return 0, false
	}
	user, err := h.Queries.GetUserByID(r.Context(), userID)
	if err != nil || !user.IsAdmin {
		writeErr(w, r, http.StatusForbidden, ErrForbidden, "admin access required")
		return 0, false
	}
	return userID, true
}

// ListPendingReports handles GET /moderation/reports (admin only).
func (h *OperationHandlers) ListPendingReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := startCursorDesc(r.URL.Query().Get("cursor"))

	rows, err := h.Queries.ListPendingReportsPage(r.Context(), store.ListPendingReportsPageParams{
		ID: before, Limit: int32(limit + 1),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]reportDTO, len(rows))
	for i, rep := range rows {
		var listingRef string
		var subscriberCount int32
		if listing, err := h.Queries.GetListingByID(r.Context(), rep.ListingID); err == nil {
			listingRef, subscriberCount = listing.ListingRef, listing.SubscriberCount
		}
		items[i] = reportToDTO(rep, listingRef, subscriberCount)
	}
	var nextCursor *string
	if hasMore {
		c := encodeCursor(rows[len(rows)-1].ID)
		nextCursor = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

type resolveReportRequest struct {
	Action string `json:"action"` // dismiss | takedown
}

// ResolveReport handles POST /moderation/reports/{id}/resolve (admin
// only). action=takedown disables the underlying resource — new run
// creation and new subscriptions immediately hit the existing
// "resource disabled" check (30002/run_authorizer.go), which is how
// existing subscribers lose access; action=dismiss just closes the report.
func (h *OperationHandlers) ResolveReport(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}
	var req resolveReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Action != "dismiss" && req.Action != "takedown") {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed",
			[]FieldError{{Field: "action", Reason: "must be one of dismiss, takedown"}})
		return
	}

	report, err := h.Queries.GetReportByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrReportNotFound, "举报不存在")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if report.Status != "pending" {
		writeErr(w, r, http.StatusConflict, ErrReportAlreadyResolved, "该举报已处理")
		return
	}

	if req.Action == "takedown" {
		listing, err := h.Queries.GetListingByID(r.Context(), report.ListingID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		if err := h.disableUnderlyingResource(r.Context(), listing.ResourceType, listing.ResourceID); err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		if err := h.Queries.SetListingDistribution(r.Context(), store.SetListingDistributionParams{ID: listing.ID, Distribution: 3}); err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		detail, _ := json.Marshal(map[string]any{"listing_ref": listing.ListingRef, "subscriber_count": listing.SubscriberCount})
		_, _ = h.Queries.CreateAuditLog(r.Context(), store.CreateAuditLogParams{
			ActorUserID: pgtype.Int8{Valid: true, Int64: adminID}, Action: "moderation.takedown",
			TargetType: "marketplace_listing", TargetID: strconv.FormatInt(listing.ID, 10), Detail: detail,
		})
	}

	resolved, err := h.Queries.ResolveReport(r.Context(), store.ResolveReportParams{
		ID: id, Resolution: pgtype.Text{Valid: true, String: req.Action}, ResolvedBy: pgtype.Int8{Valid: true, Int64: adminID},
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	var listingRef string
	var subscriberCount int32
	if listing, err := h.Queries.GetListingByID(r.Context(), resolved.ListingID); err == nil {
		listingRef, subscriberCount = listing.ListingRef, listing.SubscriberCount
	}
	writeJSON(w, r, http.StatusOK, reportToDTO(resolved, listingRef, subscriberCount))
}

func (h *OperationHandlers) disableUnderlyingResource(ctx context.Context, resourceType string, resourceID int64) error {
	switch resourceType {
	case string(marketplace.KindAgent):
		return h.Queries.SetAgentStatusByID(ctx, store.SetAgentStatusByIDParams{ID: resourceID, Status: 0})
	case string(marketplace.KindBundle):
		return h.Queries.SetBundleStatusByID(ctx, store.SetBundleStatusByIDParams{ID: resourceID, Status: 0})
	case string(marketplace.KindSkill):
		return h.Queries.SetSkillStatusByID(ctx, store.SetSkillStatusByIDParams{ID: resourceID, Status: 0})
	case string(marketplace.KindMCP):
		return h.Queries.SetMCPServerStatusByID(ctx, store.SetMCPServerStatusByIDParams{ID: resourceID, Status: 0})
	default:
		return nil
	}
}
