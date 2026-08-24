-- Seeds 模型广场 with the same illustrative models the old hand-maintained
-- Go literal (internal/domain/modelcenter.Catalog, now removed) used to
-- show, so switching the catalog to be DB-backed doesn't leave the page
-- empty on a fresh install. An admin can edit/disable any of this from
-- 系统配置 → 模型提供商 afterwards — it's just a starting point.

INSERT INTO catalog_providers (provider_key, display_name) VALUES
    ('anthropic', 'Anthropic'),
    ('openai', 'OpenAI'),
    ('google', 'Google'),
    ('deepseek', 'DeepSeek'),
    ('qwen', '通义千问');

INSERT INTO catalog_models (provider_id, model, display_name, description, modality, featured) VALUES
    ((SELECT id FROM catalog_providers WHERE provider_key = 'anthropic'), 'claude-sonnet-5', 'Claude Sonnet 5', '均衡的日常旗舰，编排与工具调用能力强', 'text', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'anthropic'), 'claude-opus-5', 'Claude Opus 5', '复杂推理与长链路任务', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'anthropic'), 'claude-haiku-4-5', 'Claude Haiku 4.5', '响应快，适合高频轻量任务', 'text', false),

    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'gpt-4o', 'GPT-4o', '文本 + 视觉多模态旗舰，支持图像理解与描述', 'vision', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'gpt-4o-mini', 'GPT-4o mini', '更低成本的日常选择', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'dall-e-3', 'DALL·E 3', '文本生成图像', 'image', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'text-embedding-3-small', 'OpenAI Embedding 3 Small', '1536 维，知识库检索首选', 'embedding', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'text-embedding-3-large', 'OpenAI Embedding 3 Large', '更高精度，维度需与知识库配置匹配', 'embedding', false),

    ((SELECT id FROM catalog_providers WHERE provider_key = 'google'), 'gemini-1.5-pro', 'Gemini 1.5 Pro', '长上下文与多模态推理，支持图像/视频帧理解', 'vision', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'google'), 'gemini-1.5-flash', 'Gemini 1.5 Flash', '低延迟、低成本', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'google'), 'veo-2', 'Veo 2', '文本生成视频', 'video', false),

    ((SELECT id FROM catalog_providers WHERE provider_key = 'deepseek'), 'deepseek-reasoner', 'DeepSeek Reasoner', '显式推理链路，擅长数学与代码', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'deepseek'), 'deepseek-chat', 'DeepSeek Chat', '性价比高的通用对话模型', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'deepseek'), 'deepseek-v3', 'DeepSeek V3', '新一代旗舰基座模型，通用能力全面提升', 'text', true),

    ((SELECT id FROM catalog_providers WHERE provider_key = 'qwen'), 'qwen-max', '通义千问 Max', '千问系列旗舰，中文语境表现突出', 'text', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'qwen'), 'qwen-plus', '通义千问 Plus', '效果与成本的折中', 'text', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'qwen'), 'qwen-vl-plus', '通义千问 VL Plus', '图文理解，中文场景优化', 'vision', false),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'qwen'), 'text-embedding-v2', '通义千问 Embedding V2', 'DashScope 兼容模式下的向量模型', 'embedding', false);
