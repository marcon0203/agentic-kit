-- 模型提供商 = 可调用的渠道。
--
-- 在这之前，系统配置 → 模型提供商 建出来的只是"模型广场里的一个分组"，
-- provider_key 是自由文本，而真正能调的渠道硬编在 Go 里的一张表上。两者
-- 名字对上纯属巧合——管理员建一个新的提供商，Agent 里引用它必然报"没有这
-- 个 client"。
--
-- 现在把两者合成一个：建提供商时选一个协议模板（见
-- internal/channeltemplates），渲染出的渠道描述符存在 descriptor 里，进程
-- 启动和每次增删改后加载进 modelgateway 的注册表。
--
-- descriptor 存的是**快照**而不是模板引用：以后模板改了，已经建好的渠道
-- 不受影响。渠道的行为不该在管理员毫不知情的情况下随一次升级变掉。
ALTER TABLE catalog_providers
    ADD COLUMN template   VARCHAR(64),
    ADD COLUMN descriptor JSONB;

-- 内置提供商全部下掉：模型供应商是部署方的配置，不是平台的产品内容。预置
-- 一堆没有凭据、可能还调不通的渠道，只会让人以为"平台自带这些能力"。
-- 迁移前建的行没有 descriptor，留着也调不了，一并清掉。
DELETE FROM catalog_models;
DELETE FROM catalog_providers;
