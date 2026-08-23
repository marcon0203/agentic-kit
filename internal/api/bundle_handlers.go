package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// BundleHandlers implements GET/POST /bundles and DELETE /bundles/{ref}
// per api/openapi.yaml.
type BundleHandlers struct {
	Queries   store.Querier
	Validator *dslschema.Validator
}

func NewBundleHandlers(q store.Querier, validator *dslschema.Validator) *BundleHandlers {
	return &BundleHandlers{Queries: q, Validator: validator}
}

type bundleDTO struct {
	ID         string    `json:"id"`
	BundleRef  string    `json:"bundle_ref"`
	Version    string    `json:"version"`
	Definition any       `json:"definition"`
	Status     int16     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func toBundleDTO(row store.Bundle) (bundleDTO, error) {
	var def any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return bundleDTO{}, err
	}
	return bundleDTO{
		ID:         strconv.FormatInt(row.ID, 10),
		BundleRef:  row.BundleRef,
		Version:    row.Version,
		Definition: def,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt.Time,
	}, nil
}

// List handles GET /bundles — one row per bundle_ref (its latest version).
func (h *BundleHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	afterRef := ""
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := decodeCursorString(cursor)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
			return
		}
		afterRef = decoded
	}

	rows, err := h.Queries.ListBundlesForOwnerLatestPage(r.Context(), store.ListBundlesForOwnerLatestPageParams{
		OwnerUserID: userID, BundleRef: afterRef, Limit: int32(limit + 1),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]bundleDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := toBundleDTO(row)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		items = append(items, dto)
	}

	var nextCursor *string
	if len(rows) > 0 {
		c := encodeCursorString(rows[len(rows)-1].BundleRef)
		nextCursor = &c
	}

	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

type createBundleRequest struct {
	Definition map[string]any `json:"definition"`
}

// Create handles POST /bundles: Schema validation, then graph static
// validation (spec-07's core value) — entry/edge-target existence and
// self-loop-aware deadlock detection are blocking (422 + 40003);
// unreachable nodes and handoff-contract mismatches are warnings returned
// alongside the created Bundle so the save still succeeds.
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

	schemaErrs, err := h.Validator.Validate(req.Definition)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if len(schemaErrs) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrBundleSchemaInvalid, "Bundle 定义不符合 Schema", toAPIFieldErrors(schemaErrs))
		return
	}

	graph, err := bundlegraph.ParseGraph(req.Definition)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrBundleSchemaInvalid, "malformed orchestration block")
		return
	}

	result := bundlegraph.Validate(graph)
	if !result.Valid() {
		writeErrDetails(w, r, http.StatusUnprocessableEntity, ErrBundleGraphInvalid, "Bundle 编排图存在无法触发的节点", toAPIIssues(result.Errors))
		return
	}

	handoffWarnings, err := checkHandoffConsistency(r.Context(), h.Queries, userID, req.Definition, graph)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	result.Warnings = append(result.Warnings, handoffWarnings...)

	bundleRef, _ := req.Definition["bundle"].(string)
	version, _ := req.Definition["version"].(string)
	definitionBytes, err := json.Marshal(req.Definition)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	row, err := h.Queries.CreateBundle(r.Context(), store.CreateBundleParams{
		OwnerUserID: userID, BundleRef: bundleRef, Version: version,
		Definition: definitionBytes, DisplayMeta: []byte("{}"),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, r, http.StatusConflict, ErrResourceRefDuplicate, "this bundle_ref/version already exists")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	dto, err := toBundleDTO(row)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	if len(result.Warnings) == 0 {
		writeJSON(w, r, http.StatusCreated, dto)
		return
	}
	writeJSON(w, r, http.StatusCreated, createBundleResponse{bundleDTO: dto, Warnings: toAPIIssues(result.Warnings)})
}

// createBundleResponse extends the plain Bundle response with graph
// warnings (spec-07 points 4-5) when there are any, so the caller can
// surface them without a second request.
type createBundleResponse struct {
	bundleDTO
	Warnings []FieldError `json:"warnings,omitempty"`
}

func (b createBundleResponse) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID         string       `json:"id"`
		BundleRef  string       `json:"bundle_ref"`
		Version    string       `json:"version"`
		Definition any          `json:"definition"`
		Status     int16        `json:"status"`
		CreatedAt  time.Time    `json:"created_at"`
		Warnings   []FieldError `json:"warnings,omitempty"`
	}
	return json.Marshal(alias{
		ID: b.ID, BundleRef: b.BundleRef, Version: b.Version,
		Definition: b.Definition, Status: b.Status, CreatedAt: b.CreatedAt,
		Warnings: b.Warnings,
	})
}

// Delete handles DELETE /bundles/{ref}.
func (h *BundleHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	ref := chi.URLParam(r, "ref")

	subscribedCount, err := h.Queries.CountActiveSubscribedListingsForBundleRef(r.Context(), store.CountActiveSubscribedListingsForBundleRefParams{OwnerUserID: userID, BundleRef: ref})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if subscribedCount > 0 {
		writeErr(w, r, http.StatusConflict, ErrSubscribedVersionLocked, "该 Bundle 的某个版本已被订阅，无法删除，只能停止分发")
		return
	}

	if err := h.Queries.DeleteBundlesByRef(r.Context(), store.DeleteBundlesByRefParams{OwnerUserID: userID, BundleRef: ref}); err != nil {
		writeErr(w, r, http.StatusConflict, ErrSubscribedVersionLocked, "该版本已被订阅，不可删除（快照隔离）")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toAPIIssues(issues []bundlegraph.Issue) []FieldError {
	out := make([]FieldError, len(issues))
	for i, iss := range issues {
		out[i] = FieldError{Field: iss.Field, Reason: iss.Message}
	}
	return out
}
