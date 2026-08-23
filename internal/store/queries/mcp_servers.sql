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
