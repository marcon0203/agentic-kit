-- 管理员统一凭证：系统配置 → 模型提供商可以为一个 Provider 登记组织级默认
-- api_key + base_url，和用户各自在 /models 页面接入的个人凭证是两套东西，
-- 只在用户自己没有为该 provider 配置个人凭证时才会被用作兜底（见
-- postgres.ProviderKeyStore.Keys）。加密值和用户凭证共用同一把 AES-256 密
-- 钥，永不以明文返回给前端。
ALTER TABLE catalog_providers
    ADD COLUMN default_api_key_encrypted TEXT;
