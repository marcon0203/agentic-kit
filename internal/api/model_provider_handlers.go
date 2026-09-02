package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

// ProviderAccessReader reports which providers the caller can actually run
// with. Deliberately names only — the credentials behind them never reach
// the transport layer, so there is nothing here for a handler to leak.
type ProviderAccessReader interface {
	UsableProviders(ctx context.Context, ownerID int64) ([]string, error)
}

// ModelProviderHandlers is the HTTP transport for provider registration
// (spec-09). Note what the DTO does not have: there is no credential
// field, because modelcenter.Provider has no credential field either.
type ModelProviderHandlers struct {
	svc    *modelcenter.Service
	access ProviderAccessReader
}

func NewModelProviderHandlers(svc *modelcenter.Service, access ProviderAccessReader) *ModelProviderHandlers {
	return &ModelProviderHandlers{svc: svc, access: access}
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

type modelAccessDTO struct {
	Providers   []string `json:"providers"`
	HasProvider bool     `json:"has_provider"`
}

// MyAccess handles GET /me/model-access — the providers this account can
// actually run with, personal connections and an admin's org-wide defaults
// alike. Every "can I start a run?" gate in the UI reads this rather than
// GET /model-providers: that endpoint lists only what the user registered
// themselves, so an account running purely on an org-wide default was told
// it had no provider at all and had its 运行 buttons disabled, while the
// backend would have run the same Bundle happily.
func (h *ModelProviderHandlers) MyAccess(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	if h.access == nil {
		writeJSON(w, r, http.StatusOK, modelAccessDTO{Providers: []string{}})
		return
	}

	providers, err := h.access.UsableProviders(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	if providers == nil {
		providers = []string{}
	}
	writeJSON(w, r, http.StatusOK, modelAccessDTO{Providers: providers, HasProvider: len(providers) > 0})
}

// Specs handles GET /model-provider-specs — 平台支持的模型渠道以及每个渠
// 道要哪几个凭据字段。
//
// 它的来源是 modelgateway 的渠道注册表（内置声明式描述符 + 少数手写
// client），前端据此渲染"新增模型提供商"的表单和显示名，而不是在前端再抄
// 一份渠道列表——抄第二份的下场是每加一个渠道就有一处忘了改。
//
// 不需要鉴权范围之外的东西：这里只有渠道的形状，没有任何凭据值。
func (h *ModelProviderHandlers) Specs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": modelgateway.ProviderSpecs()})
}
