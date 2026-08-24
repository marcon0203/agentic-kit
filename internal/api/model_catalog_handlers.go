package api

import (
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcatalog"
)

// ModelCatalogHandlers serves 模型广场's browsable model catalog — display
// data joined from admin-configured catalog_providers/catalog_models
// (系统配置 → 模型提供商), not a binding contract: an Agent's model.name
// field stays free text (spec-09), this just gives a user something to
// browse by modality/provider before typing one in.
type ModelCatalogHandlers struct {
	svc *modelcatalog.Service
}

func NewModelCatalogHandlers(svc *modelcatalog.Service) *ModelCatalogHandlers {
	return &ModelCatalogHandlers{svc: svc}
}

type modelCatalogEntryDTO struct {
	Provider            string `json:"provider"`
	ProviderDisplayName string `json:"provider_display_name"`
	ProviderIcon        string `json:"provider_icon,omitempty"`
	Model               string `json:"model"`
	DisplayName         string `json:"display_name"`
	Description         string `json:"description"`
	Modality            string `json:"modality"`
	Featured            bool   `json:"featured"`
}

// List handles GET /model-catalog.
func (h *ModelCatalogHandlers) List(w http.ResponseWriter, r *http.Request) {
	entries, err := h.svc.List(r.Context())
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]modelCatalogEntryDTO, 0, len(entries))
	for _, e := range entries {
		items = append(items, modelCatalogEntryDTO{
			Provider: e.ProviderKey, ProviderDisplayName: e.ProviderDisplayName, ProviderIcon: e.ProviderIcon,
			Model: e.Model, DisplayName: e.DisplayName, Description: e.Description,
			Modality: string(e.Modality), Featured: e.Featured,
		})
	}
	writeJSON(w, r, http.StatusOK, items)
}
