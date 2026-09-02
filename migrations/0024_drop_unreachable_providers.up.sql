-- 下线 anthropic / openai 两个渠道：在本部署的网络环境下调不通，留着只会
-- 让用户配了 key 也用不了，还占着模型广场的位置。
--
-- 顺带补上火山引擎方舟。方舟的模型是"推理接入点"，每个账号自己开、ID 各
-- 不相同（ep-xxxxxxxx），所以这里只登记 provider 不预置具体模型——预置一
-- 个别人账号下不存在的接入点 ID，比空着更容易误导。
--
-- 用户已经存的 anthropic/openai 凭据一并删除：provider 名从
-- components.schemas.ProviderName 里去掉之后，那些行既选不中也校验不过，
-- 留着就是一堆再也无法通过接口访问的密文。
DELETE FROM catalog_models
WHERE provider_id IN (SELECT id FROM catalog_providers WHERE provider_key IN ('anthropic', 'openai'));

DELETE FROM catalog_providers WHERE provider_key IN ('anthropic', 'openai');

DELETE FROM model_providers WHERE provider IN ('anthropic', 'openai');

INSERT INTO catalog_providers (provider_key, display_name)
VALUES ('volcengine', '火山引擎方舟')
ON CONFLICT (provider_key) DO NOTHING;
