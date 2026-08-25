-- name: CreatePlugin :one
INSERT INTO plugins (plugin_id, version, manifest, oss_prefix, publisher_id, signature, visibility, review_status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPluginVersion :one
SELECT * FROM plugins WHERE plugin_id = $1 AND version = $2;

-- name: GetLatestPluginVersion :one
-- 未指定版本时用的"最新" — 按 created_at 取最近发布的一个版本，
-- 和 GetBundleLatestByRef 的既有约定一致。
SELECT * FROM plugins WHERE plugin_id = $1 AND status = 1 ORDER BY created_at DESC LIMIT 1;

-- name: ListPluginVersions :many
SELECT * FROM plugins WHERE plugin_id = $1 ORDER BY created_at DESC;

-- name: ListMarketPlugins :many
-- 组件广场"插件" Tab 用的市场列表：只列公开且审核通过的，每个 plugin_id 一行（最新版本）。
SELECT DISTINCT ON (plugin_id) *
FROM plugins
WHERE visibility = 'public' AND review_status = 'passed' AND status = 1
ORDER BY plugin_id, created_at DESC;

-- name: ListPluginsByPublisher :many
SELECT * FROM plugins WHERE publisher_id = $1 ORDER BY plugin_id, created_at DESC;

-- name: SetPluginVisibility :one
UPDATE plugins SET visibility = $2 WHERE id = $1 RETURNING *;

-- name: ListPendingReviewPlugins :many
SELECT * FROM plugins WHERE review_status = 'pending' AND visibility = 'public' ORDER BY created_at ASC;

-- name: SetPluginReviewStatus :one
UPDATE plugins SET review_status = $2 WHERE id = $1 RETURNING *;

-- name: CreatePluginInstallation :one
INSERT INTO plugin_installations (owner_user_id, plugin_id, version, resolution, config, granted)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPluginInstallation :one
SELECT * FROM plugin_installations WHERE owner_user_id = $1 AND plugin_id = $2;

-- name: ListPluginInstallations :many
SELECT * FROM plugin_installations WHERE owner_user_id = $1 AND status = 1 ORDER BY plugin_id;

-- name: UpdatePluginInstallation :one
UPDATE plugin_installations
SET version = $3, resolution = $4, config = $5, granted = $6
WHERE owner_user_id = $1 AND plugin_id = $2
RETURNING *;

-- name: DeletePluginInstallation :execrows
DELETE FROM plugin_installations WHERE owner_user_id = $1 AND plugin_id = $2;

-- name: GetPublisherKey :one
SELECT * FROM plugin_publisher_keys WHERE user_id = $1;

-- name: UpsertPublisherKey :one
INSERT INTO plugin_publisher_keys (user_id, public_key)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET public_key = EXCLUDED.public_key
RETURNING *;
