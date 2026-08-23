package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/operation"
)

// OperationHandlers is the HTTP transport for the 运营中心 context
// (spec-18): a user's own audit trail, submitting a report, and the
// admin-only report queue.
type OperationHandlers struct {
	svc *operation.Service
}

func NewOperationHandlers(svc *operation.Service) *OperationHandlers {
	return &OperationHandlers{svc: svc}
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

func toAuditLogDTO(e operation.AuditEntry) auditLogDTO {
	return auditLogDTO{
		ID: strconv.FormatInt(e.ID, 10), Action: e.Action, TargetType: e.TargetType,
		TargetID: e.TargetID, Detail: e.Detail, CreatedAt: e.CreatedAt,
	}
}

// ListMyAuditLogs handles GET /audit-logs.
func (h *OperationHandlers) ListMyAuditLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	after, err := cursorAfterString(r)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
		return
	}

	page, err := h.svc.ListMyAuditLog(r.Context(), userID, domain.PageQuery{
		Limit: parseLimit(r.URL.Query().Get("limit")), After: after,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toAuditLogDTO))
}

// ── Reports ──────────────────────────────────────────────────────────

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

func toReportDTO(v operation.ReportView) reportDTO {
	dto := reportDTO{
		ID: strconv.FormatInt(v.Report.ID, 10), ListingRef: v.Listing.Ref, Reason: v.Report.Reason,
		Status: string(v.Report.Status), SubscriberCount: v.Listing.SubscriberCount,
		CreatedAt: v.Report.CreatedAt, ResolvedAt: v.Report.ResolvedAt,
	}
	if v.Report.Resolution != nil {
		res := string(*v.Report.Resolution)
		dto.Resolution = &res
	}
	return dto
}

// SubmitReport handles POST /marketplace/listings/{ref}/report.
func (h *OperationHandlers) SubmitReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed",
			[]FieldError{{Field: "reason", Reason: "required"}})
		return
	}

	view, err := h.svc.SubmitReport(r.Context(), userID, chi.URLParam(r, "ref"), req.Reason)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toReportDTO(view))
}

// ListPendingReports handles GET /moderation/reports (admin only).
func (h *OperationHandlers) ListPendingReports(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	after, err := cursorAfterString(r)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
		return
	}

	page, err := h.svc.ListPendingReports(r.Context(), userID, domain.PageQuery{
		Limit: parseLimit(r.URL.Query().Get("limit")), After: after,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toReportDTO))
}

type resolveReportRequest struct {
	Action string `json:"action"` // dismiss | takedown
}

// ResolveReport handles POST /moderation/reports/{id}/resolve (admin only).
func (h *OperationHandlers) ResolveReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}

	var req resolveReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed",
			[]FieldError{{Field: "action", Reason: "must be one of dismiss, takedown"}})
		return
	}

	view, err := h.svc.ResolveReport(r.Context(), userID, id, req.Action)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toReportDTO(view))
}
