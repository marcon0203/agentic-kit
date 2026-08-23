package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ModelProviderHandlers implements GET/POST /model-providers per
// api/openapi.yaml and spec-09-model-gateway.md. Credentials are
// AES-256-GCM encrypted at rest and never echoed back on any response —
// every DTO here is built without ever touching the credentials column.
type ModelProviderHandlers struct {
	Queries store.Querier
	AESKey  []byte
	// NewValidator returns the connectivity Validator for a provider name,
	// or nil for an unrecognized one. Overridable in tests; defaults to
	// modelgateway.NewValidator (real Anthropic/OpenAI/Google endpoints).
	NewValidator func(provider string) modelgateway.Validator
}

func NewModelProviderHandlers(q store.Querier, aesKey []byte) *ModelProviderHandlers {
	return &ModelProviderHandlers{Queries: q, AESKey: aesKey, NewValidator: modelgateway.NewValidator}
}

type modelProviderDTO struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Status    int16     `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// List handles GET /model-providers. The underlying query's own SELECT
// list already excludes `credentials` — there is no column to
// accidentally serialize here.
func (h *ModelProviderHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	rows, err := h.Queries.ListModelProvidersForOwner(r.Context(), userID)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	items := make([]modelProviderDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelProviderDTO{
			ID: strconv.FormatInt(row.ID, 10), Provider: row.Provider, Status: row.Status, CreatedAt: row.CreatedAt.Time,
		})
	}
	writeJSON(w, r, http.StatusOK, items)
}

type createModelProviderRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
}

// Create handles POST /model-providers: validates the credential actually
// authenticates against the named Provider before ever storing it
// (spec-09's "Provider 接入时的连通性校验"), then encrypts and stores it.
// The plaintext key exists only for the duration of this request — it is
// never logged and never appears in the response.
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

	var details []FieldError
	if req.Provider == "" {
		details = append(details, FieldError{Field: "provider", Reason: "required"})
	}
	if req.APIKey == "" {
		details = append(details, FieldError{Field: "api_key", Reason: "required"})
	}
	if len(details) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid request", details)
		return
	}

	validator := h.NewValidator(req.Provider)
	if validator == nil {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid request",
			[]FieldError{{Field: "provider", Reason: "must be one of anthropic, openai, google, custom"}})
		return
	}
	if err := validator.Validate(r.Context(), req.APIKey); err != nil {
		writeErr(w, r, http.StatusUnprocessableEntity, ErrProviderCredsInvalid, "凭证无效："+err.Error())
		return
	}

	encrypted, err := crypto.Encrypt(h.AESKey, []byte(req.APIKey))
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	row, err := h.Queries.CreateModelProvider(r.Context(), store.CreateModelProviderParams{
		OwnerUserID: userID, Provider: req.Provider, Credentials: []byte(encrypted),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	writeJSON(w, r, http.StatusCreated, modelProviderDTO{
		ID: strconv.FormatInt(row.ID, 10), Provider: row.Provider, Status: row.Status, CreatedAt: row.CreatedAt.Time,
	})
}
