package modelcenter

// Modality is what kind of input/output a catalog model works with — the
// dimension 模型广场 lets a user browse by, per spec (视觉/音频/文本/
// embedding 等等模型), rather than the flat "which Provider did I
// register" list this context used to be limited to.
type Modality string

const (
	ModalityText      Modality = "text"
	ModalityVision    Modality = "vision"
	ModalityEmbedding Modality = "embedding"
)

// CatalogCategory groups catalog entries the way 模型广场's sections do
// (深度思考/文本生成/视觉理解/向量模型, ...) — a finer cut than Modality,
// which only says input/output shape, not what the model is *for*.
type CatalogCategory string

const (
	CategoryReasoning CatalogCategory = "reasoning"
	CategoryText      CatalogCategory = "text"
	CategoryVision    CatalogCategory = "vision"
	CategoryEmbedding CatalogCategory = "embedding"
)

// CatalogEntry is one illustrative model this platform's Providers can
// reach. It is display data, not a binding contract — an Agent's
// model.name field stays free text (spec-09), so nothing here restricts
// which model string a user actually types; the catalog exists so
// 模型广场 has something to show before that decision, not to enumerate
// every model a Provider ever ships.
type CatalogEntry struct {
	Provider    string
	Model       string
	DisplayName string
	Description string
	Modality    Modality
	Category    CatalogCategory
	Featured    bool
}

// Catalog reuses the exact model names internal/modelgateway/pricing.go
// already prices (claude-sonnet-5/opus-5/haiku-4-5, gpt-4o/gpt-4o-mini,
// gemini-1.5-pro/flash) so the platform names one canonical set of
// examples instead of two that could drift apart, plus DeepSeek/Qwen/
// embedding entries for the providers added alongside them.
var Catalog = []CatalogEntry{
	// 首推 — one flagship text model per provider family.
	{Provider: "anthropic", Model: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", Description: "均衡的日常旗舰，编排与工具调用能力强", Modality: ModalityText, Category: CategoryText, Featured: true},
	{Provider: "openai", Model: "gpt-4o", DisplayName: "GPT-4o", Description: "文本 + 视觉多模态旗舰", Modality: ModalityVision, Category: CategoryText, Featured: true},
	{Provider: "qwen", Model: "qwen-max", DisplayName: "通义千问 Max", Description: "千问系列旗舰，中文语境表现突出", Modality: ModalityText, Category: CategoryText, Featured: true},

	// 深度思考 — models pitched at harder reasoning tasks.
	{Provider: "anthropic", Model: "claude-opus-5", DisplayName: "Claude Opus 5", Description: "复杂推理与长链路任务", Modality: ModalityText, Category: CategoryReasoning},
	{Provider: "deepseek", Model: "deepseek-reasoner", DisplayName: "DeepSeek Reasoner", Description: "显式推理链路，擅长数学与代码", Modality: ModalityText, Category: CategoryReasoning},
	{Provider: "google", Model: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro", Description: "长上下文与多模态推理", Modality: ModalityVision, Category: CategoryReasoning},

	// 文本生成 — everyday chat/completion models.
	{Provider: "anthropic", Model: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", Description: "响应快，适合高频轻量任务", Modality: ModalityText, Category: CategoryText},
	{Provider: "openai", Model: "gpt-4o-mini", DisplayName: "GPT-4o mini", Description: "更低成本的日常选择", Modality: ModalityText, Category: CategoryText},
	{Provider: "deepseek", Model: "deepseek-chat", DisplayName: "DeepSeek Chat", Description: "性价比高的通用对话模型", Modality: ModalityText, Category: CategoryText},
	{Provider: "qwen", Model: "qwen-plus", DisplayName: "通义千问 Plus", Description: "效果与成本的折中", Modality: ModalityText, Category: CategoryText},
	{Provider: "google", Model: "gemini-1.5-flash", DisplayName: "Gemini 1.5 Flash", Description: "低延迟、低成本", Modality: ModalityText, Category: CategoryText},

	// 视觉理解
	{Provider: "qwen", Model: "qwen-vl-plus", DisplayName: "通义千问 VL Plus", Description: "图文理解，中文场景优化", Modality: ModalityVision, Category: CategoryVision},
	{Provider: "openai", Model: "gpt-4o", DisplayName: "GPT-4o 视觉", Description: "图像理解与描述", Modality: ModalityVision, Category: CategoryVision},
	{Provider: "google", Model: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro 视觉", Description: "图像 · 视频帧理解", Modality: ModalityVision, Category: CategoryVision},

	// 向量模型 — embeddings, for 知识库 retrieval (internal/domain/knowledgebase).
	{Provider: "openai", Model: "text-embedding-3-small", DisplayName: "OpenAI Embedding 3 Small", Description: "1536 维，知识库检索首选", Modality: ModalityEmbedding, Category: CategoryEmbedding, Featured: true},
	{Provider: "openai", Model: "text-embedding-3-large", DisplayName: "OpenAI Embedding 3 Large", Description: "更高精度，维度需与知识库配置匹配", Modality: ModalityEmbedding, Category: CategoryEmbedding},
	{Provider: "qwen", Model: "text-embedding-v2", DisplayName: "通义千问 Embedding V2", Description: "DashScope 兼容模式下的向量模型", Modality: ModalityEmbedding, Category: CategoryEmbedding},
}
