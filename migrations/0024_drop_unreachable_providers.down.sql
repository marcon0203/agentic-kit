-- 回滚只恢复渠道目录本身。被删掉的用户凭据不可恢复——它们是加密存储的密
-- 文，删了就没了；回滚一个删除迁移不该假装能把数据变回来。
DELETE FROM catalog_models
WHERE provider_id IN (SELECT id FROM catalog_providers WHERE provider_key = 'volcengine');
DELETE FROM catalog_providers WHERE provider_key = 'volcengine';

INSERT INTO catalog_providers (provider_key, display_name) VALUES
    ('anthropic', 'Anthropic'),
    ('openai', 'OpenAI')
ON CONFLICT (provider_key) DO NOTHING;

INSERT INTO catalog_models (provider_id, model, display_name, description, modality, featured) VALUES
    ((SELECT id FROM catalog_providers WHERE provider_key = 'anthropic'), 'claude-sonnet-5', 'Claude Sonnet 5', '均衡的日常旗舰，编排与工具调用能力强', 'text', true),
    ((SELECT id FROM catalog_providers WHERE provider_key = 'openai'), 'gpt-4o', 'GPT-4o', '文本 + 视觉多模态旗舰，支持图像理解与描述', 'vision', true)
ON CONFLICT DO NOTHING;
