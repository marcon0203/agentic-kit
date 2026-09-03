package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// ResourceHandlers is the HTTP transport for the 资源中心 context. Credential
// handling, the four-kind fan-out and the MCP probe rule all live in
// internal/domain/resource.
type ResourceHandlers struct {
	svc      *resource.Service
	mcpProbe resource.ToolProbe // backs POST /resources/mcp/probe; nil-safe
}

func NewResourceHandlers(svc *resource.Service, mcpProbe resource.ToolProbe) *ResourceHandlers {
	return &ResourceHandlers{svc: svc, mcpProbe: mcpProbe}
}

type resourceDTO struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Ref         string         `json:"ref"`
	DisplayName string         `json:"display_name,omitempty"`
	Config      map[string]any `json:"config"`
	Status      int16          `json:"status"`
	Health      string         `json:"health,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func toResourceDTO(r resource.Resource) resourceDTO {
	config := map[string]any(r.Config)
	if config == nil {
		config = map[string]any{}
	}
	return resourceDTO{
		ID:          encodeResourceID(r.Kind, r.ID),
		Type:        string(r.Kind),
		Ref:         r.Ref,
		DisplayName: r.DisplayName,
		Config:      config,
		Status:      int16(r.Status),
		Health:      string(r.Health),
		CreatedAt:   r.CreatedAt,
	}
}

// List handles GET /resources.
func (h *ResourceHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	page, err := h.svc.List(r.Context(), userID, resource.ListQuery{
		Kind:  r.URL.Query().Get("type"),
		Limit: parseLimit(r.URL.Query().Get("limit")),
		After: decodeCursor(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toResourceDTO))
}

type createResourceRequest struct {
	Type        string         `json:"type"`
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

	created, err := h.svc.Create(r.Context(), userID, resource.CreateCommand{
		Kind: req.Type, Ref: req.Ref, DisplayName: req.DisplayName, Config: resource.Config(req.Config),
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toResourceDTO(created))
}

type updateResourceRequest struct {
	DisplayName *string        `json:"display_name"`
	Config      map[string]any `json:"config"`
	Status      *int16         `json:"status"`
}

// Update handles PATCH /resources/{id} (also enable/disable via `status`).
// Get handles GET /resources/{id}：单条资源详情。
//
// 领域层的 Service.Get 一直都有，只是没有这条路由——于是前端只能靠列表接
// 口凑详情，或者干脆做不了详情页。config 里的凭据字段照例不会出现
// （Redact），编辑时不回填即表示"不改"。
func (h *ResourceHandlers) Get(w http.ResponseWriter, r *http.Request) {
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
	res, err := h.svc.Get(r.Context(), userID, kind, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toResourceDTO(res))
}

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

	var req updateResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	cmd := resource.UpdateCommand{DisplayName: req.DisplayName, Config: resource.Config(req.Config)}
	if req.Status != nil {
		status := resource.Status(*req.Status)
		cmd.Status = &status
	}

	updated, err := h.svc.Update(r.Context(), userID, kind, id, cmd)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toResourceDTO(updated))
}

type resourceReferenceDTO struct {
	Type    string `json:"type"`
	Ref     string `json:"ref"`
	Version string `json:"version"`
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

	check, err := h.svc.DeleteCheck(r.Context(), userID, kind, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	referencedBy := make([]resourceReferenceDTO, 0, len(check.ReferencedBy))
	for _, ref := range check.ReferencedBy {
		referencedBy = append(referencedBy, resourceReferenceDTO{Type: "agent", Ref: ref.AgentRef, Version: ref.Version})
	}
	writeJSON(w, r, http.StatusOK, deleteCheckDTO{Deletable: check.Deletable, ReferencedBy: referencedBy})
}

type mcpHeaderDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type probeMCPRequest struct {
	URL     string         `json:"url"`
	Headers []mcpHeaderDTO `json:"headers"`
}

type probeMCPToolDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type probeMCPResponse struct {
	OK    bool              `json:"ok"`
	Tools []probeMCPToolDTO `json:"tools,omitempty"`
	Error string            `json:"error,omitempty"`
}

// Probe handles POST /resources/mcp/probe — a real MCP handshake against a
// server the user hasn't saved a resource for yet (or is re-checking one
// they have), so the registration page can show what tools it actually
// advertises before the "保存" button ever does anything. Never persists;
// a failed probe is still a 200 with ok:false, not a 4xx/5xx, because
// "the server rejected this" is the expected answer for a wrong URL/header,
// not a transport-level failure.
func (h *ResourceHandlers) Probe(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserIDFromContext(r.Context()); !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	if h.mcpProbe == nil {
		writeJSON(w, r, http.StatusOK, probeMCPResponse{OK: false, Error: "mcp probing is not configured on this deployment"})
		return
	}

	var req probeMCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	headers := make(map[string]string, len(req.Headers))
	for _, h := range req.Headers {
		if h.Key != "" {
			headers[h.Key] = h.Value
		}
	}

	tools, err := h.mcpProbe.Probe(r.Context(), req.URL, headers)
	if err != nil {
		writeJSON(w, r, http.StatusOK, probeMCPResponse{OK: false, Error: err.Error()})
		return
	}

	toolDTOs := make([]probeMCPToolDTO, len(tools))
	for i, t := range tools {
		toolDTOs[i] = probeMCPToolDTO{Name: t.Name, Description: t.Description}
	}
	writeJSON(w, r, http.StatusOK, probeMCPResponse{OK: true, Tools: toolDTOs})
}

// ── External resource IDs ────────────────────────────────────────────

// Resources are split across four tables, so a bare numeric id is ambiguous
// across kinds. The external id encodes the kind alongside it; base64 keeps
// it opaque so a client can't hand-craft one for a table it shouldn't reach.
func encodeResourceID(kind resource.Kind, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(string(kind) + ":" + strconv.FormatInt(id, 10)))
}

func decodeResourceID(external string) (resource.Kind, int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(external)
	if err != nil {
		return "", 0, errors.New("resource id: not base64")
	}
	kindStr, idStr, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return "", 0, errors.New("resource id: missing separator")
	}
	kind, ok := resource.ParseKind(kindStr)
	if !ok {
		return "", 0, errors.New("resource id: unknown kind")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return "", 0, errors.New("resource id: bad numeric part")
	}
	return kind, id, nil
}

// ── Shared transport helpers ─────────────────────────────────────────

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

// encodeCursorString/decodeCursorString are the string-keyed counterpart,
// used where the keyset is a ref rather than a numeric id.
func encodeCursorString(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func decodeCursorString(cursor string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
