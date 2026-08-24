package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
)

// ModelProviderHandlers is the HTTP transport for provider registration
// (spec-09). Note what the DTO does not have: there is no credential
// field, because modelcenter.Provider has no credential field either.
type ModelProviderHandlers struct {
	svc *modelcenter.Service
}

func NewModelProviderHandlers(svc *modelcenter.Service) *ModelProviderHandlers {
	return &ModelProviderHandlers{svc: svc}
}

type modelProviderDTO struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	BaseURL   *string   `json:"base_url,omitempty"`
	Status    int16     `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func toModelProviderDTO(p modelcenter.Provider) modelProviderDTO {
	dto := modelProviderDTO{
		ID: strconv.FormatInt(p.ID, 10), Provider: p.Name, Status: p.Status, CreatedAt: p.CreatedAt,
	}
	if p.BaseURL != "" {
		dto.BaseURL = &p.BaseURL
	}
	return dto
}

// List handles GET /model-providers.
func (h *ModelProviderHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	providers, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]modelProviderDTO, 0, len(providers))
	for _, p := range providers {
		items = append(items, toModelProviderDTO(p))
	}
	writeJSON(w, r, http.StatusOK, items)
}

type createModelProviderRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
}

// Create handles POST /model-providers.
func (h *ModelProviderHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createModelProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	created, err := h.svc.Register(r.Context(), userID, req.Provider, req.APIKey, req.BaseURL)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toModelProviderDTO(created))
}
