// Package modelcatalog is the admin-managed 模型广场 catalog: a system
// setting (系统配置 → 模型提供商) rather than a per-user credential like
// internal/domain/modelcenter.Provider. An admin registers a Provider
// (with a display name and icon), then registers Models under it
// (e.g. deepseek-v3), tagged with a Modality; only enabled models under
// enabled providers are what 模型广场 (GET /model-catalog) actually shows.
package modelcatalog

import "time"

// Modality is the "模型类型" tag 模型广场 lets a user filter by. It doubles
// as the shape a model works with (what goes in, what comes out) — text
// prompt/response, image generation, video generation, image+text
// understanding, or a vector embedding.
type Modality string

const (
	ModalityText      Modality = "text"
	ModalityImage     Modality = "image"
	ModalityVideo     Modality = "video"
	ModalityVision    Modality = "vision"
	ModalityEmbedding Modality = "embedding"
)

func (m Modality) Valid() bool {
	switch m {
	case ModalityText, ModalityImage, ModalityVideo, ModalityVision, ModalityEmbedding:
		return true
	}
	return false
}

// Provider is one admin-registered model vendor entry — a catalog fixture,
// not a credential. Icon is a URL or a data: URI; both render the same way
// in an <img src>, so the domain doesn't need to know which.
//
// HasCredential reports whether an admin has additionally registered an
// org-wide default api_key for this provider (SetProviderCredential) — the
// encrypted value itself never leaves the postgres adapter, so this is only
// ever a boolean flag, never the key.
type Provider struct {
	ID          int64
	Key         string
	DisplayName string
	Icon        string
	BaseURL     string
	// Template 记录这个提供商是从哪个协议模板建出来的，只供界面展示。真正
	// 决定怎么调用的是落库的描述符快照——模板以后改了，已经建好的提供商不
	// 受影响。
	Template      string
	Status        int16
	CreatedAt     time.Time
	HasCredential bool
}

func (p Provider) Enabled() bool { return p.Status == 1 }

// Model is one model registered under a Provider (e.g. deepseek-v3), the
// unit 模型广场 actually lists.
type Model struct {
	ID          int64
	ProviderID  int64
	Model       string
	DisplayName string
	Description string
	Modality    Modality
	Featured    bool
	Status      int16
	CreatedAt   time.Time
}

func (m Model) Enabled() bool { return m.Status == 1 }

// CatalogEntry is a Model joined with its Provider's display data — exactly
// what 模型广场's public listing needs to render one card.
type CatalogEntry struct {
	Model               string
	DisplayName         string
	Description         string
	Modality            Modality
	Featured            bool
	ProviderKey         string
	ProviderDisplayName string
	ProviderIcon        string
}
