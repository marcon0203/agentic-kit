-- name: CreateTool :one
INSERT INTO tools (owner_user_id, ref, version, config, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetToolByIDForOwner :one
SELECT * FROM tools WHERE id = $1 AND owner_user_id = $2;

-- name: ListToolsForOwnerPage :many
SELECT * FROM tools WHERE owner_user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3;

-- name: UpdateTool :one
UPDATE tools SET display_meta = $3, config = $4, status = $5
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: MarkToolImmutable :exec
UPDATE tools SET immutable = true WHERE id = $1;

-- name: FindAgentsReferencingToolRef :many
SELECT owner_user_id, agent_ref, version FROM agents
WHERE owner_user_id = $1
  AND (definition->'capabilities'->'tools') ? $2::text;

-- name: GetToolLatestStatusByRef :one
SELECT status FROM tools WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetToolLatestByRef :one
-- Used by the run engine (spec-10/11) to build a real tool.Tool from the
-- resource's config — capabilities.tools[] refs aren't version-pinned, so
-- this is always "whatever's currently live", same as the status lookup.
SELECT * FROM tools WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;
