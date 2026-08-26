-- 插件自己的小状态存储（spec-20 §4.3 的 kv.get/set host function）。
-- 按 (plugin_id, owner_user_id) 隔离——两个不同用户装的同一个插件，或者
-- 同一个用户装的两个不同插件，互相看不到对方的 kv 数据。
CREATE TABLE plugin_kv (
    plugin_id      VARCHAR(128) NOT NULL,
    owner_user_id  BIGINT       NOT NULL REFERENCES users(id),
    key            VARCHAR(256) NOT NULL,
    value          TEXT         NOT NULL,
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (plugin_id, owner_user_id, key)
);
