-- name: CreateAPIKey :one
INSERT INTO api_keys (owner_user_id, name, key_hash)
VALUES ($1, $2, $3)
RETURNING id, owner_user_id, name, last_used_at, revoked_at, created_at;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL;

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;

-- name: ListAPIKeysForOwner :many
SELECT id, owner_user_id, name, last_used_at, revoked_at, created_at
FROM api_keys
WHERE owner_user_id = $1
ORDER BY created_at DESC;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND owner_user_id = $2 AND revoked_at IS NULL;
