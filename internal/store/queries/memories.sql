-- name: CreateMemory :one
INSERT INTO memories (owner_user_id, ref, version, config, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMemoryByIDForOwner :one
SELECT * FROM memories WHERE id = $1 AND owner_user_id = $2;

-- name: ListMemoriesForOwnerPage :many
SELECT * FROM memories WHERE owner_user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3;

-- name: UpdateMemory :one
UPDATE memories SET display_meta = $3, config = $4, status = $5
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: GetNewestEnabledMemoryForOwner :one
-- The run engine's "which memory store backs this owner's runs" lookup —
-- newest-enabled-wins, the same rule postgres.ProviderKeyStore already
-- applies to provider credentials.
SELECT * FROM memories WHERE owner_user_id = $1 AND status = 1 ORDER BY created_at DESC LIMIT 1;
