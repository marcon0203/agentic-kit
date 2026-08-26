-- name: GetPluginKV :one
SELECT * FROM plugin_kv WHERE plugin_id = $1 AND owner_user_id = $2 AND key = $3;

-- name: UpsertPluginKV :one
INSERT INTO plugin_kv (plugin_id, owner_user_id, key, value)
VALUES ($1, $2, $3, $4)
ON CONFLICT (plugin_id, owner_user_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING *;
