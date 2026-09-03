// Package modelcatalog is the admin-managed 模型广场 catalog: a system
// setting (系统配置 → 模型提供商) rather than a per-user credential like
// internal/domain/modelcenter.Provider. An admin registers a Provider
// (with a display name and icon), then registers Models under it
// (e.g. deepseek-v3), tagged with a Modality; only enabled models under
// enabled providers are what 模型广场 (GET /model-catalog) actually shows.
package modelcatalog

import (
	"encoding/json"
	"time"

	descriptortype "github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

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
	// Descriptor 是描述符快照的原文，只在服务端用（添加/编辑模型时按它声
	// 明的 request_params 校验参数取值），永远不进任何对外 DTO。
	Descriptor []byte
}

func (p Provider) Enabled() bool { return p.Status == 1 }

// RequestParams 从描述符快照里解析出参数声明。解析失败按"没有声明"处理
// ——快照在创建时已完整校验过，这里只是防御手工改库的兜底。
func (p Provider) RequestParams() []descriptortype.RequestParam {
	var doc struct {
		RequestParams []descriptortype.RequestParam `json:"request_params"`
	}
	_ = json.Unmarshal(p.Descriptor, &doc)
	return doc.RequestParams
}

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
	// Params 是这个模型的请求参数取值（max_tokens 等），形状由所属提供商
	// 描述符快照的 request_params 声明，调用时由网关注入。
	Params map[string]any
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
	// ProviderTemplate 是建这个 Provider 用的协议模板名，图标没填时前端拿它
	// 当图标名再试一次（deepseek 模板正好配 deepseek 图标）。
	ProviderTemplate string
}
