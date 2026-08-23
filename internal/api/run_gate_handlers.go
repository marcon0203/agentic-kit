package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

type resolveGateRequest struct {
	Node     string `json:"node"`
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

// ResolveGate handles POST /runs/{id}/gate. Who may approve, what a
// decision records and how the blocked run learns about it are all the
// service's business; this only decodes and maps the outcome.
func (h *RunHandlers) ResolveGate(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req resolveGateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "node is required")
		return
	}

	err := h.svc.ResolveGate(r.Context(), userID, chi.URLParam(r, "id"), run.ResolveGateCommand{
		Node: req.Node, Approved: req.Approved, Comment: req.Comment,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
