package api

import "net/http"

// FeaturesConfig is what the frontend needs to know about server-side
// feature switches to render correctly — whether 知识库 (KB_ENABLED) is on,
// so the UI can hide the sidebar entry and the resource-kind route instead
// of offering a feature that will reject every request with a validation
// error, and whether Skill zip upload (OSS_*) is on, so the upload entry
// point can be greyed out with an explanation instead of failing at submit.
type FeaturesConfig struct {
	KnowledgeBaseEnabled bool
	SkillUploadEnabled   bool
}

type featuresDTO struct {
	KnowledgeBaseEnabled bool `json:"knowledge_base_enabled"`
	SkillUploadEnabled   bool `json:"skill_upload_enabled"`
}

// FeaturesHandler serves GET /features.
func FeaturesHandler(cfg FeaturesConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, featuresDTO{
			KnowledgeBaseEnabled: cfg.KnowledgeBaseEnabled,
			SkillUploadEnabled:   cfg.SkillUploadEnabled,
		})
	}
}
