package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/channeltemplates"
	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/modelcatalog"
	descriptortype "github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// ModelCatalogAdminHandlers is 系统配置 → 模型提供商's HTTP transport:
// registering a catalog Provider (with an icon), then registering Models
// under it. Every method requires the caller to be an admin — enforced in
// modelcatalog.Service, not here, so the 403 reason stays one source of
// truth.
type ModelCatalogAdminHandlers struct {
	svc *modelcatalog.Service
}

func NewModelCatalogAdminHandlers(svc *modelcatalog.Service) *ModelCatalogAdminHandlers {
	return &ModelCatalogAdminHandlers{svc: svc}
}

type catalogProviderDTO struct {
	ID            string    `json:"id"`
	Key           string    `json:"key"`
	DisplayName   string    `json:"display_name"`
	Icon          string    `json:"icon,omitempty"`
	BaseURL       string    `json:"base_url,omitempty"`
	Template      string    `json:"template,omitempty"`
	Status        int16     `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	HasCredential bool      `json:"has_credential"`
	// RequestParams 是这个渠道线协议声明的模型级参数（快照里的
	// request_params）。添加模型表单按它渲染输入项——描述符原文不透出，
	// 只透出表单需要的这一段。
	RequestParams []descriptortype.RequestParam `json:"request_params"`
}

func toCatalogProviderDTO(p modelcatalog.Provider) catalogProviderDTO {
	return catalogProviderDTO{
		ID: strconv.FormatInt(p.ID, 10), Key: p.Key, DisplayName: p.DisplayName,
		Icon: p.Icon, BaseURL: p.BaseURL, Template: p.Template, Status: p.Status, CreatedAt: p.CreatedAt,
		HasCredential: p.HasCredential, RequestParams: p.RequestParams(),
	}
}

type catalogModelDTO struct {
	ID          string         `json:"id"`
	ProviderID  string         `json:"provider_id"`
	Model       string         `json:"model"`
	DisplayName string         `json:"display_name"`
	Description string         `json:"description"`
	Modality    string         `json:"modality"`
	Featured    bool           `json:"featured"`
	Status      int16          `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	Params      map[string]any `json:"params"`
}

func toCatalogModelDTO(m modelcatalog.Model) catalogModelDTO {
	return catalogModelDTO{
		ID: strconv.FormatInt(m.ID, 10), ProviderID: strconv.FormatInt(m.ProviderID, 10),
		Model: m.Model, DisplayName: m.DisplayName, Description: m.Description,
		Modality: string(m.Modality), Featured: m.Featured, Status: m.Status, CreatedAt: m.CreatedAt,
		Params: m.Params,
	}
}

func requireUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return 0, false
	}
	return userID, true
}

func parseIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid "+name)
		return 0, false
	}
	return id, true
}

// ListProviders handles GET /model-catalog/providers.
func (h *ModelCatalogAdminHandlers) ListProviders(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	providers, err := h.svc.ListProviders(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]catalogProviderDTO, 0, len(providers))
	for _, p := range providers {
		items = append(items, toCatalogProviderDTO(p))
	}
	writeJSON(w, r, http.StatusOK, items)
}

type createCatalogProviderRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
	BaseURL     string `json:"base_url"`
	// Template 是协议模板 id（GET /model-channel-templates）。必填：新建一
	// 个模型提供商的实质就是"从模板实例化一个可调用的渠道"。
	Template string `json:"template"`
}

// CreateProvider handles POST /model-catalog/providers.
func (h *ModelCatalogAdminHandlers) CreateProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createCatalogProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	created, err := h.svc.CreateProvider(r.Context(), userID, modelcatalog.NewProvider{
		Key: req.Key, DisplayName: req.DisplayName, Icon: req.Icon,
		BaseURL: req.BaseURL, Template: req.Template,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toCatalogProviderDTO(created))
}

type setStatusRequest struct {
	Status int16 `json:"status"`
}

// UpdateProviderStatus handles PATCH /model-catalog/providers/{id}.
func (h *ModelCatalogAdminHandlers) UpdateProviderStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req setStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	if err := h.svc.SetProviderStatus(r.Context(), userID, id, req.Status == 1); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

// DeleteProvider handles DELETE /model-catalog/providers/{id}.
func (h *ModelCatalogAdminHandlers) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteProvider(r.Context(), userID, id); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

type setCatalogProviderCredentialRequest struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

// SetProviderCredential handles PUT /model-catalog/providers/{id}/credential
// — the admin-managed org-wide default api_key + base_url, layered
// alongside (never replacing) each user's own /models connection.
// APIKey empty leaves the currently stored key untouched, so re-saving
// base_url alone doesn't force the admin to re-paste an already-set key.
func (h *ModelCatalogAdminHandlers) SetProviderCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req setCatalogProviderCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	if err := h.svc.SetProviderCredential(r.Context(), userID, id, req.APIKey, req.BaseURL); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

// ListModels handles GET /model-catalog/providers/{id}/models.
func (h *ModelCatalogAdminHandlers) ListModels(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	providerID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	models, err := h.svc.ListModelsForProvider(r.Context(), userID, providerID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]catalogModelDTO, 0, len(models))
	for _, m := range models {
		items = append(items, toCatalogModelDTO(m))
	}
	writeJSON(w, r, http.StatusOK, items)
}

type createCatalogModelRequest struct {
	Model       string `json:"model"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Modality    string `json:"modality"`
	Featured    bool   `json:"featured"`
	// Params 是模型级请求参数的取值，形状按所属提供商声明的 request_params
	// 校验（线协议必填的参数——如 Anthropic 的 max_tokens——在这一步收齐）。
	Params map[string]any `json:"params"`
}

// CreateModel handles POST /model-catalog/providers/{id}/models.
func (h *ModelCatalogAdminHandlers) CreateModel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	providerID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req createCatalogModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	created, err := h.svc.CreateModel(r.Context(), userID, modelcatalog.NewModel{
		ProviderID: providerID, Model: req.Model, DisplayName: req.DisplayName,
		Description: req.Description, Modality: modelcatalog.Modality(req.Modality),
		Featured: req.Featured, Params: req.Params,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toCatalogModelDTO(created))
}

type updateCatalogModelRequest struct {
	Status *int16 `json:"status"`
	// Params 用指针区分"这次请求没带 params"和"带了个空对象（清掉全部参
	// 数）"。清掉后如果线协议有必填参数，UpdateModelParams 的校验会拦下。
	Params *map[string]any `json:"params"`
}

// UpdateModel handles PATCH /model-catalog/providers/{id}/models/{model_id}:
// status 和 params 是两个独立的可更新项，一次请求里带哪个改哪个。
func (h *ModelCatalogAdminHandlers) UpdateModelStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	modelID, ok := parseIDParam(w, r, "model_id")
	if !ok {
		return
	}
	var req updateCatalogModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	if req.Status != nil {
		if err := h.svc.SetModelStatus(r.Context(), userID, modelID, *req.Status == 1); err != nil {
			writeDomainErr(w, r, err)
			return
		}
	}
	if req.Params != nil {
		if err := h.svc.UpdateModelParams(r.Context(), userID, modelID, *req.Params); err != nil {
			writeDomainErr(w, r, err)
			return
		}
	}
	writeJSON(w, r, http.StatusOK, nil)
}

// DeleteModel handles DELETE /model-catalog/providers/{id}/models/{model_id}.
func (h *ModelCatalogAdminHandlers) DeleteModel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	modelID, ok := parseIDParam(w, r, "model_id")
	if !ok {
		return
	}
	if err := h.svc.DeleteModel(r.Context(), userID, modelID); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

// ListChannelTemplates handles GET /model-channel-templates：可选的协议模
// 板列表。
//
// 平台开箱不带任何模型供应商——供应商是部署方的配置，不是平台的产品内容。
// 但从零写一份渠道描述符对非开发者太难，所以给一组写好线协议的模板：管理
// 员挑一个、填上自己的 key 和接口地址就能用。
func (h *ModelCatalogAdminHandlers) ListChannelTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	items, err := channeltemplates.List()
	if err != nil {
		writeDomainErr(w, r, domain.Internal(err))
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}
