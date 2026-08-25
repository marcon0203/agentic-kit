-- 插件体系（spec-20）：一个插件包（.akp）多个版本，装到某个账号下才生效。
-- 和其余四种资源不同，插件是第三方代码而不是配置，所以需要版本历史和
-- 安装关系，走独立的两张表，而不是塞进已有的四张资源表（spec-05a 的
-- "不加列，字段收在 config 里"这条只适用于配置类资源）。
CREATE TABLE plugins (
    id             BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    plugin_id      VARCHAR(128) NOT NULL,        -- 反向域名式，如 acme.charts
    version        VARCHAR(32)  NOT NULL,        -- 语义化版本
    manifest       JSONB        NOT NULL,        -- plugin.json 原文，schemas/plugin.schema.json 校验过的
    oss_prefix     TEXT         NOT NULL,        -- 包内容（plugin.wasm/ui/assets）在 OSS 的位置
    publisher_id   BIGINT       REFERENCES users(id), -- NULL = 平台内置
    signature      TEXT         NOT NULL,        -- 发布时的签名，装载前校验（spec-20 §5.3）
    visibility     VARCHAR(16)  NOT NULL DEFAULT 'private', -- private | public
    review_status  VARCHAR(16)  NOT NULL DEFAULT 'pending', -- pending | passed | rejected
    status         SMALLINT     NOT NULL DEFAULT 1, -- 1 启用 2 停用，同 resources 表的既有约定
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (plugin_id, version)
);
CREATE INDEX plugins_publisher_idx ON plugins (publisher_id);
CREATE INDEX plugins_market_idx ON plugins (visibility, review_status) WHERE status = 1;

-- 谁把哪个插件的哪个版本装到了自己账号下——一个账号同一个插件只能装一份，
-- 要换版本走 UPDATE，不是并存多份。
CREATE TABLE plugin_installations (
    id             BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    owner_user_id  BIGINT       NOT NULL REFERENCES users(id),
    plugin_id      VARCHAR(128) NOT NULL,
    version        VARCHAR(32)  NOT NULL,        -- 装的时候锁定的版本
    resolution     VARCHAR(8)   NOT NULL DEFAULT 'pinned', -- pinned | live，spec-20 §2.1
    config         JSONB        NOT NULL DEFAULT '{}', -- 插件自己的配置/凭证，走既有的加密约定
    granted        JSONB        NOT NULL DEFAULT '{}', -- 用户实际授予的 requires.permissions 子集
    status         SMALLINT     NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, plugin_id),
    FOREIGN KEY (plugin_id, version) REFERENCES plugins (plugin_id, version)
);
CREATE INDEX plugin_installations_owner_idx ON plugin_installations (owner_user_id, status);

-- 发布者用来对自己上传的插件包签名的 Ed25519 公钥（spec-20 §5.3）。私钥
-- 从不接触服务端——发布者本地签名，这里只存验签用的公钥，一个账号一把。
CREATE TABLE plugin_publisher_keys (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id),
    public_key   BYTEA        NOT NULL,  -- 32 字节 Ed25519 公钥
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
