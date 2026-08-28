package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/agent"
)

// AgentHandlers is the HTTP transport for the Agent context: decode, call
// the service, encode. Every rule about what an Agent may reference, when a
// version may be deleted and which business code a failure carries lives in
// internal/domain/agent — none of it here.
type AgentHandlers struct {
	svc *agent.Service
}

func NewAgentHandlers(svc *agent.Service) *AgentHandlers { return &AgentHandlers{svc: svc} }

type agentDTO struct {
	ID         string    `json:"id"`
	AgentRef   string    `json:"agent_ref"`
	Version    string    `json:"version"`
	Definition any       `json:"definition"`
	Status     int16     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func toAgentDTO(a agent.Agent) agentDTO {
	return agentDTO{
		ID:         strconv.FormatInt(a.ID, 10),
		AgentRef:   a.Ref,
		Version:    a.Version,
		Definition: map[string]any(a.Definition),
		Status:     int16(a.Status),
		CreatedAt:  a.CreatedAt,
	}
}

func toAgentDTOs(agents []agent.Agent) []agentDTO {
	out := make([]agentDTO, 0, len(agents))
	for _, a := range agents {
		out = append(out, toAgentDTO(a))
	}
	return out
}

// List handles GET /agents — one row per agent_ref (its latest version).
func (h *AgentHandlers) List(w http.ResponseWriter, r *http.Request) {
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
	writeDomainPage(w, r, mapPage(page, toAgentDTO))
}

type createAgentRequest struct {
	Definition map[string]any `json:"definition"`
}

// Create handles POST /agents — a new Agent, or a new version of an existing
// one (definition.agent is the ref).
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

	created, err := h.svc.Create(r.Context(), userID, agent.Definition(req.Definition))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toAgentDTO(created))
}

// Update handles PATCH /agents/{id} — edits the latest version by creating a
// new version with an auto-bumped version number. Routes by numeric id, not
// the DSL's agent key, so definition.agent no longer has to match the path.
func (h *AgentHandlers) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}

	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	updated, err := h.svc.Update(r.Context(), userID, id, agent.Definition(req.Definition))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toAgentDTO(updated))
}

// ListVersions handles GET /agents/{id}/versions.
func (h *AgentHandlers) ListVersions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}

	versions, err := h.svc.ListVersions(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toAgentDTOs(versions))
}

// Delete handles DELETE /agents/{id}.
func (h *AgentHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
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
