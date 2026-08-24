package api

import (
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
)

// ModelCatalogHandlers serves 模型广场's browsable model catalog — display
// data (internal/domain/modelcenter.Catalog), not a binding contract: an
// Agent's model.name field stays free text (spec-09), this just gives a
// user something to browse by modality before typing one in.
type ModelCatalogHandlers struct{}

func NewModelCatalogHandlers() *ModelCatalogHandlers { return &ModelCatalogHandlers{} }

type modelCatalogEntryDTO struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Modality    string `json:"modality"`
	Category    string `json:"category"`
	Featured    bool   `json:"featured"`
}

// List handles GET /model-catalog.
func (h *ModelCatalogHandlers) List(w http.ResponseWriter, r *http.Request) {
	items := make([]modelCatalogEntryDTO, 0, len(modelcenter.Catalog))
	for _, e := range modelcenter.Catalog {
		items = append(items, modelCatalogEntryDTO{
			Provider: e.Provider, Model: e.Model, DisplayName: e.DisplayName,
			Description: e.Description, Modality: string(e.Modality),
			Category: string(e.Category), Featured: e.Featured,
		})
	}
	writeJSON(w, r, http.StatusOK, items)
}
