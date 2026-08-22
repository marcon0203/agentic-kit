-- name: CreateModelProvider :one
INSERT INTO model_providers (owner_user_id, provider, credentials)
VALUES ($1, $2, $3)
RETURNING id, owner_user_id, provider, status, created_at;

-- name: ListModelProvidersForOwner :many
SELECT id, owner_user_id, provider, status, created_at
FROM model_providers
WHERE owner_user_id = $1
ORDER BY created_at DESC;

-- name: GetModelProviderCredentials :one
SELECT credentials FROM model_providers WHERE id = $1 AND owner_user_id = $2;

-- name: SetModelProviderStatus :exec
UPDATE model_providers SET status = $2 WHERE id = $1;
