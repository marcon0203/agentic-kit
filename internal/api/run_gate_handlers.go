package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

type resolveGateRequest struct {
	Node     string `json:"node"`
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

// ResolveGate handles POST /runs/{id}/gate. spec-11: "必须做审批人角色的
// 二次校验" — V1 has no team/role system, so the only person ever
// authorized to resolve a gate is the run's own triggered_by user (see
// migrations/0010's human_gates.approver_roles comment for the same
// rationale). Every decision is additionally logged to audit_logs
// (append-only, spec-11's "审批记录额外落一份到 audit_logs").
func (h *RunHandlers) ResolveGate(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	runID := chi.URLParam(r, "id")

	var req resolveGateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Node == "" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "node is required")
		return
	}

	run, err := h.Queries.GetBundleRun(r.Context(), runID)
	if err != nil {
		if pgxErrNoRows(err) {
			writeErr(w, r, http.StatusNotFound, ErrRunNotFound, "run not found")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if run.TriggeredBy != userID {
		writeErr(w, r, http.StatusForbidden, ErrGateApproverForbidden, "非指定审批角色")
		return
	}
	if run.Status != "running" {
		writeErr(w, r, http.StatusConflict, ErrRunAlreadyFinished, "run 已结束")
		return
	}

	gate, err := h.Queries.GetPendingHumanGateForRunNode(r.Context(), store.GetPendingHumanGateForRunNodeParams{RunID: runID, Node: req.Node})
	if err != nil {
		if pgxErrNoRows(err) {
			writeErr(w, r, http.StatusConflict, ErrGateAlreadyResolved, "gate 已被处理")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	status := "rejected"
	if req.Approved {
		status = "approved"
	}
	var comment pgtype.Text
	if req.Comment != "" {
		comment = pgtype.Text{String: req.Comment, Valid: true}
	}
	if _, err := h.Queries.ResolveHumanGate(r.Context(), store.ResolveHumanGateParams{
		ID: gate.ID, Status: status, ResolvedBy: pgtype.Int8{Valid: true, Int64: userID}, Comment: comment,
	}); err != nil {
		if pgxErrNoRows(err) {
			// Lost a race with the timeout scanner or a concurrent call.
			writeErr(w, r, http.StatusConflict, ErrGateAlreadyResolved, "gate 已被处理")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	detail, _ := json.Marshal(map[string]any{"run_id": runID, "node": req.Node, "approved": req.Approved, "comment": req.Comment})
	_, _ = h.Queries.CreateAuditLog(r.Context(), store.CreateAuditLogParams{
		ActorUserID: pgtype.Int8{Valid: true, Int64: userID}, Action: "human_gate." + status,
		TargetType: "human_gate", TargetID: strconv.FormatInt(gate.ID, 10), Detail: detail,
	})

	reason := "rejected by user"
	if req.Comment != "" {
		reason = req.Comment
	}
	h.Engine.Gates.resolve(gate.ID, gateResolution{approved: req.Approved, reason: reason})

	w.WriteHeader(http.StatusNoContent)
}
