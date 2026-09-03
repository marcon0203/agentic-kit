package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/apikey"
)

// APIKeyHandlers is the HTTP transport for 系统配置 → API Key 管理: every
// call is scoped to the caller's own keys (whichever auth scheme got them
// in — a JWT session managing keys for future ApiKey-authenticated calls
// is the normal case).
type APIKeyHandlers struct{ svc *apikey.Service }

func NewAPIKeyHandlers(svc *apikey.Service) *APIKeyHandlers { return &APIKeyHandlers{svc: svc} }

type apiKeySummaryDTO struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Revoked    bool       `json:"revoked"`
	CreatedAt  time.Time  `json:"created_at"`
}

type apiKeyCreatedDTO struct {
	apiKeySummaryDTO
	RawKey string `json:"raw_key"`
}

func toAPIKeySummaryDTO(k apikey.APIKey) apiKeySummaryDTO {
	return apiKeySummaryDTO{
		ID: k.ID, Name: k.Name, LastUsedAt: k.LastUsedAt, Revoked: !k.Active(), CreatedAt: k.CreatedAt,
	}
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

// Create handles POST /api-keys. The response is the only place the raw
// key ever appears — it is not retrievable through List afterward.
func (h *APIKeyHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	created, err := h.svc.Create(r.Context(), userID, req.Name)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, apiKeyCreatedDTO{
		apiKeySummaryDTO: toAPIKeySummaryDTO(created.APIKey), RawKey: created.RawKey,
	})
}

// List handles GET /api-keys.
func (h *APIKeyHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	keys, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]apiKeySummaryDTO, 0, len(keys))
	for _, k := range keys {
		items = append(items, toAPIKeySummaryDTO(k))
	}
	writeJSON(w, r, http.StatusOK, items)
}

// Revoke handles DELETE /api-keys/{id}.
func (h *APIKeyHandlers) Revoke(w http.ResponseWriter, r *http.Request) {
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
	if err := h.svc.Revoke(r.Context(), userID, id); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
