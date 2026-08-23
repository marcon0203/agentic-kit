package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marcon0203/agentic-kit/internal/store"
)

var resourceRefPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ResourceHandlers implements GET/POST /resources, PATCH /resources/{id}
// and GET /resources/{id}/delete-check per api/openapi.yaml, dispatching to
// one of four per-kind stores (spec-05-resource-center.md "分表设计").
type ResourceHandlers struct {
	Kinds  map[ResourceKind]resourceKindStore
	MCP    MCPHealthChecker
	AESKey []byte
}

func NewResourceHandlers(q *store.Queries, mcp MCPHealthChecker, aesKey []byte) *ResourceHandlers {
	return &ResourceHandlers{
		Kinds: map[ResourceKind]resourceKindStore{
			ResourceKindTool:          toolStore{q: q},
			ResourceKindSkill:         skillStore{q: q},
			ResourceKindMCP:           mcpServerStore{q: q},
			ResourceKindKnowledgeBase: knowledgeBaseStore{q: q},
		},
		MCP:    mcp,
		AESKey: aesKey,
	}
}

type resourceDTO struct {
	ID          string         `json:"id"`
	Type        ResourceKind   `json:"type"`
	Ref         string         `json:"ref"`
	DisplayName string         `json:"display_name,omitempty"`
	Config      map[string]any `json:"config"`
	Status      int16          `json:"status"`
	Health      string         `json:"health,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func toResourceDTO(kind ResourceKind, row resourceRow) resourceDTO {
	var displayMeta map[string]any
	_ = json.Unmarshal(row.DisplayMeta, &displayMeta)
	displayName, _ := displayMeta["display_name"].(string)

	var config map[string]any
	_ = json.Unmarshal(row.Config, &config)

	return resourceDTO{
		ID:          encodeResourceID(kind, row.ID),
		Type:        kind,
		Ref:         row.Ref,
		DisplayName: displayName,
		Config:      RedactConfigForResponse(config),
		Status:      row.Status,
		Health:      row.Health,
		CreatedAt:   row.CreatedAt.Time,
	}
}

// List handles GET /resources. With ?type= set, lists that kind with real
// cursor pagination. Without it, merges a first page from all four kinds —
// see resource_kind.go's AllResourceKinds doc comment: this is a
// deliberate V1 simplification, not a true cross-table cursor.
func (h *ResourceHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	afterID := decodeCursor(r.URL.Query().Get("cursor"))
	typeParam := ResourceKind(r.URL.Query().Get("type"))

	kindsToQuery := AllResourceKinds
	if typeParam != "" {
		if !isValidResourceKind(typeParam) {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "unknown resource type")
			return
		}
		kindsToQuery = []ResourceKind{typeParam}
	}

	var items []resourceDTO
	hasMore := false
	var nextCursor *string

	for _, kind := range kindsToQuery {
		rows, err := h.Kinds[kind].ListPage(r.Context(), userID, afterID, int32(limit+1))
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		if len(rows) > limit {
			hasMore = true
			rows = rows[:limit]
		}
		for _, row := range rows {
			items = append(items, toResourceDTO(kind, row))
		}
		if len(kindsToQuery) == 1 && len(rows) > 0 {
			c := encodeCursor(rows[len(rows)-1].ID)
			nextCursor = &c
		}
	}

	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

type createResourceRequest struct {
	Type        ResourceKind   `json:"type"`
	Ref         string         `json:"ref"`
	DisplayName string         `json:"display_name"`
	Config      map[string]any `json:"config"`
}

// Create handles POST /resources.
func (h *ResourceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	var details []FieldError
	if !isValidResourceKind(req.Type) {
		details = append(details, FieldError{Field: "type", Message: "must be one of tool, skill, mcp, knowledge_base"})
	}
	if !resourceRefPattern.MatchString(req.Ref) {
		details = append(details, FieldError{Field: "ref", Message: "must match ^[a-z][a-z0-9_-]*$"})
	}
	if req.Config == nil {
		details = append(details, FieldError{Field: "config", Message: "required"})
	}
	if len(details) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed", details)
		return
	}

	encryptedConfig, err := EncryptConfigCredentials(h.AESKey, req.Config)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid config")
		return
	}
	configBytes, err := json.Marshal(encryptedConfig)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	displayMetaBytes, err := json.Marshal(map[string]any{"display_name": req.DisplayName})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	row, err := h.Kinds[req.Type].Create(r.Context(), userID, req.Ref, "1.0", configBytes, displayMetaBytes)
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, r, http.StatusConflict, ErrResourceRefDuplicate, "a resource with this ref already exists")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	// MCP connectivity probe: spec-05 says a failed check still allows the
	// resource to save (so the owner can come back and fix the config),
	// it just leaves health=unhealthy for the caller to notice.
	if req.Type == ResourceKindMCP {
		health := h.MCP.Check(r.Context(), req.Config)
		if mcpStore, ok := h.Kinds[ResourceKindMCP].(mcpServerStore); ok {
			_ = mcpStore.SetHealth(r.Context(), row.ID, health)
		}
		row.Health = health
	}

	writeJSON(w, r, http.StatusCreated, toResourceDTO(req.Type, row))
}

type updateResourceRequest struct {
	DisplayName *string        `json:"display_name"`
	Config      map[string]any `json:"config"`
	Status      *int16         `json:"status"`
}

// Update handles PATCH /resources/{id} (also used to enable/disable via
// `status`).
func (h *ResourceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	kind, id, err := decodeResourceID(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}
	kindStore, ok := h.Kinds[kind]
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	current, err := kindStore.GetByID(r.Context(), id, userID)
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	var req updateResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	var displayMeta map[string]any
	_ = json.Unmarshal(current.DisplayMeta, &displayMeta)
	if displayMeta == nil {
		displayMeta = map[string]any{}
	}
	if req.DisplayName != nil {
		displayMeta["display_name"] = *req.DisplayName
	}
	displayMetaBytes, err := json.Marshal(displayMeta)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	configBytes := current.Config
	if req.Config != nil {
		encrypted, err := EncryptConfigCredentials(h.AESKey, req.Config)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid config")
			return
		}
		configBytes, err = json.Marshal(encrypted)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
	}

	status := current.Status
	if req.Status != nil {
		status = *req.Status
	}

	updated, err := kindStore.Update(r.Context(), id, userID, displayMetaBytes, configBytes, status)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	writeJSON(w, r, http.StatusOK, toResourceDTO(kind, updated))
}

type resourceReferenceDTO struct {
	Type    ResourceKind `json:"type"`
	Ref     string       `json:"ref"`
	Version string       `json:"version"`
}

type deleteCheckDTO struct {
	Deletable    bool                   `json:"deletable"`
	ReferencedBy []resourceReferenceDTO `json:"referenced_by"`
}

// DeleteCheck handles GET /resources/{id}/delete-check.
func (h *ResourceHandlers) DeleteCheck(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	kind, id, err := decodeResourceID(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}
	kindStore, ok := h.Kinds[kind]
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	resource, err := kindStore.GetByID(r.Context(), id, userID)
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	refs, err := kindStore.FindReferencingAgents(r.Context(), userID, resource.Ref)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	referencedBy := make([]resourceReferenceDTO, 0, len(refs))
	for _, ref := range refs {
		referencedBy = append(referencedBy, resourceReferenceDTO{Type: "agent", Ref: ref.AgentRef, Version: ref.Version})
	}

	writeJSON(w, r, http.StatusOK, deleteCheckDTO{
		Deletable:    len(referencedBy) == 0,
		ReferencedBy: referencedBy,
	})
}

func parseLimit(raw string) int {
	const def, max = 20, 100
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func encodeCursor(lastID int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(lastID, 10)))
}

func decodeCursor(cursor string) int64 {
	if cursor == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	id, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
