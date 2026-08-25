package api

import "net/http"

// FeaturesConfig is what the frontend needs to know about server-side
// feature switches to render correctly — right now just whether 知识库
// (KB_ENABLED) is on, so the UI can hide the sidebar entry and the
// resource-kind route instead of offering a feature that will reject
// every request with a validation error.
type FeaturesConfig struct {
	KnowledgeBaseEnabled bool
}

type featuresDTO struct {
	KnowledgeBaseEnabled bool `json:"knowledge_base_enabled"`
}

// FeaturesHandler serves GET /features.
func FeaturesHandler(cfg FeaturesConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, featuresDTO{KnowledgeBaseEnabled: cfg.KnowledgeBaseEnabled})
	}
}
