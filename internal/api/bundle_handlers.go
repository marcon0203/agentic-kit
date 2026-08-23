package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
)

// BundleHandlers is the HTTP transport for the Bundle context.
type BundleHandlers struct {
	svc *bundle.Service
}

func NewBundleHandlers(svc *bundle.Service) *BundleHandlers { return &BundleHandlers{svc: svc} }

type bundleDTO struct {
	ID         string    `json:"id"`
	BundleRef  string    `json:"bundle_ref"`
	Version    string    `json:"version"`
	Definition any       `json:"definition"`
	Status     int16     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func toBundleDTO(b bundle.Bundle) bundleDTO {
	return bundleDTO{
		ID:         strconv.FormatInt(b.ID, 10),
		BundleRef:  b.Ref,
		Version:    b.Version,
		Definition: map[string]any(b.Definition),
		Status:     int16(b.Status),
		CreatedAt:  b.CreatedAt,
	}
}

// createBundleResponse extends the Bundle response with graph warnings
// (spec-07 points 4-5) when there are any, so a caller sees them without a
// second request. Flattened explicitly rather than by embedding, because an
// embedded struct plus a sibling field does not marshal flat.
type createBundleResponse struct {
	ID         string       `json:"id"`
	BundleRef  string       `json:"bundle_ref"`
	Version    string       `json:"version"`
	Definition any          `json:"definition"`
	Status     int16        `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
	Warnings   []FieldError `json:"warnings,omitempty"`
}

func toCreateBundleResponse(result bundle.CreateResult) createBundleResponse {
	dto := toBundleDTO(result.Bundle)
	resp := createBundleResponse{
		ID: dto.ID, BundleRef: dto.BundleRef, Version: dto.Version,
		Definition: dto.Definition, Status: dto.Status, CreatedAt: dto.CreatedAt,
	}
	for _, warn := range result.Warnings {
		resp.Warnings = append(resp.Warnings, FieldError{Field: warn.Field, Reason: warn.Reason})
	}
	return resp
}

// List handles GET /bundles — one row per bundle_ref (its latest version).
func (h *BundleHandlers) List(w http.ResponseWriter, r *http.Request) {
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

	page, err := h.svc.List(r.Context(), userID, domain.PageQuery{
		Limit: parseLimit(r.URL.Query().Get("limit")), After: after,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toBundleDTO))
}

type createBundleRequest struct {
	Definition map[string]any `json:"definition"`
}

// Create handles POST /bundles.
func (h *BundleHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	result, err := h.svc.Create(r.Context(), userID, bundle.Definition(req.Definition))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	// Without warnings the response is the plain Bundle shape, so a caller
	// that never looks at warnings sees exactly what it always did.
	if len(result.Warnings) == 0 {
		writeJSON(w, r, http.StatusCreated, toBundleDTO(result.Bundle))
		return
	}
	writeJSON(w, r, http.StatusCreated, toCreateBundleResponse(result))
}

// Delete handles DELETE /bundles/{ref}.
func (h *BundleHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	if err := h.svc.Delete(r.Context(), userID, chi.URLParam(r, "ref")); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
