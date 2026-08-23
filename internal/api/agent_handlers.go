package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/dslschema"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// AgentHandlers implements GET/POST /agents, DELETE /agents/{ref} and
// GET /agents/{ref}/versions per api/openapi.yaml.
type AgentHandlers struct {
	Queries   store.Querier
	Validator *dslschema.Validator
	RefCheck  *ResourceRefChecker
}

func NewAgentHandlers(q store.Querier, validator *dslschema.Validator, refCheck *ResourceRefChecker) *AgentHandlers {
	return &AgentHandlers{Queries: q, Validator: validator, RefCheck: refCheck}
}

type agentDTO struct {
	ID         string    `json:"id"`
	AgentRef   string    `json:"agent_ref"`
	Version    string    `json:"version"`
	Definition any       `json:"definition"`
	Status     int16     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func toAgentDTO(row store.Agent) (agentDTO, error) {
	var def any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return agentDTO{}, err
	}
	return agentDTO{
		ID:         strconv.FormatInt(row.ID, 10),
		AgentRef:   row.AgentRef,
		Version:    row.Version,
		Definition: def,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt.Time,
	}, nil
}

// List handles GET /agents — one row per agent_ref (its latest version).
func (h *AgentHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	afterRef := r.URL.Query().Get("cursor")
	if afterRef != "" {
		decoded, err := decodeCursorString(afterRef)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
			return
		}
		afterRef = decoded
	}

	rows, err := h.Queries.ListAgentsForOwnerLatestPage(r.Context(), store.ListAgentsForOwnerLatestPageParams{
		OwnerUserID: userID, AgentRef: afterRef, Limit: int32(limit + 1),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]agentDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := toAgentDTO(row)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		items = append(items, dto)
	}

	var nextCursor *string
	if len(rows) > 0 {
		c := encodeCursorString(rows[len(rows)-1].AgentRef)
		nextCursor = &c
	}

	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

type createAgentRequest struct {
	Definition map[string]any `json:"definition"`
}

// Create handles POST /agents — creates a new Agent, or a new version of an
// existing one (definition.agent is the ref).
func (h *AgentHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createAgentRequest
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
		writeErrDetails(w, r, http.StatusBadRequest, ErrAgentSchemaInvalid, "Agent 定义不符合 Schema", toAPIFieldErrors(schemaErrs))
		return
	}

	agentRef, _ := req.Definition["agent"].(string)
	version, _ := req.Definition["version"].(string)

	tools := toStringSlice(deepGet(req.Definition, "capabilities", "tools"))
	skills := toStringSlice(deepGet(req.Definition, "capabilities", "skills"))
	refErrs, err := h.RefCheck.CheckCapabilities(r.Context(), userID, tools, skills)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if len(refErrs) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrResourceDisabled, "Agent 引用了不存在或已禁用的资源", refErrs)
		return
	}

	definitionBytes, err := json.Marshal(req.Definition)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	row, err := h.Queries.CreateAgent(r.Context(), store.CreateAgentParams{
		OwnerUserID: userID, AgentRef: agentRef, Version: version,
		Definition: definitionBytes, DisplayMeta: []byte("{}"),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, r, http.StatusConflict, ErrResourceRefDuplicate, "this agent_ref/version already exists")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	dto, err := toAgentDTO(row)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusCreated, dto)
}

// ListVersions handles GET /agents/{ref}/versions.
func (h *AgentHandlers) ListVersions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	ref := chi.URLParam(r, "ref")

	rows, err := h.Queries.ListAgentVersionsForOwner(r.Context(), store.ListAgentVersionsForOwnerParams{OwnerUserID: userID, AgentRef: ref})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if len(rows) == 0 {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "agent not found")
		return
	}

	items := make([]agentDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := toAgentDTO(row)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		items = append(items, dto)
	}
	writeJSON(w, r, http.StatusOK, items)
}

// Delete handles DELETE /agents/{ref}. Rejects if any version is
// subscribed-and-active (70005) or referenced by a Bundle (40004); the DB's
// immutable trigger (migration 0006) is the last line of defense if either
// check races with a concurrent subscribe.
func (h *AgentHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	ref := chi.URLParam(r, "ref")

	subscribedCount, err := h.Queries.CountActiveSubscribedListingsForAgentRef(r.Context(), store.CountActiveSubscribedListingsForAgentRefParams{OwnerUserID: userID, AgentRef: ref})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if subscribedCount > 0 {
		writeErr(w, r, http.StatusConflict, ErrSubscribedVersionLocked, "该 Agent 的某个版本已被订阅，无法删除，只能停止分发")
		return
	}

	bundleRefs, err := h.Queries.FindBundlesReferencingAgentRef(r.Context(), store.FindBundlesReferencingAgentRefParams{OwnerUserID: userID, Column2: ref})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if len(bundleRefs) > 0 {
		details := make([]FieldError, 0, len(bundleRefs))
		for _, b := range bundleRefs {
			details = append(details, FieldError{Field: "referenced_by", Reason: "Bundle " + b.BundleRef + " v" + b.Version + " 正在引用"})
		}
		writeErrDetails(w, r, http.StatusConflict, ErrAgentVersionNotFound, "该 Agent 正被其他 Bundle 引用，无法删除", details)
		return
	}

	if err := h.Queries.DeleteAgentsByRef(r.Context(), store.DeleteAgentsByRefParams{OwnerUserID: userID, AgentRef: ref}); err != nil {
		writeErr(w, r, http.StatusConflict, ErrSubscribedVersionLocked, "该版本已被订阅，不可删除（快照隔离）")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toAPIFieldErrors(errs []dslschema.FieldError) []FieldError {
	out := make([]FieldError, len(errs))
	for i, e := range errs {
		out[i] = FieldError{Field: e.Field, Reason: e.Message}
	}
	return out
}

func deepGet(m map[string]any, path ...string) any {
	var cur any = m
	for _, key := range path {
		asMap, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = asMap[key]
	}
	return cur
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
