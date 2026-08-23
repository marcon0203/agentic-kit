-- name: CreateMCPServer :one
INSERT INTO mcp_servers (owner_user_id, ref, version, config, display_meta, health)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMCPServerByIDForOwner :one
SELECT * FROM mcp_servers WHERE id = $1 AND owner_user_id = $2;

-- name: ListMCPServersForOwnerPage :many
SELECT * FROM mcp_servers WHERE owner_user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3;

-- name: UpdateMCPServer :one
UPDATE mcp_servers SET display_meta = $3, config = $4, status = $5
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: UpdateMCPServerHealth :exec
UPDATE mcp_servers SET health = $2 WHERE id = $1;

-- name: MarkMCPServerImmutable :exec
UPDATE mcp_servers SET immutable = true WHERE id = $1;

-- name: FindAgentsReferencingMCPServerRef :many
SELECT owner_user_id, agent_ref, version FROM agents
WHERE owner_user_id = $1
  AND (definition->'capabilities'->'tools') ? $2::text;

-- name: GetMCPServerLatestStatusByRef :one
SELECT status FROM mcp_servers WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetMCPServerLatestByRef :one
SELECT * FROM mcp_servers WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetMCPServerByRefVersionForOwner :one
SELECT * FROM mcp_servers WHERE owner_user_id = $1 AND ref = $2 AND version = $3;

-- name: SetMCPServerDisplayMeta :exec
UPDATE mcp_servers SET display_meta = $2 WHERE id = $1;
