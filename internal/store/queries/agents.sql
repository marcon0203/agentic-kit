-- Owner-view and subscriber-view queries are kept physically separate so the
-- subscriber path can never accidentally SELECT `definition` (see
-- docs/架构设计文档_AI-Agent平台_V1.md 五、"黑盒发布的实现要点").

-- name: CreateAgent :one
INSERT INTO agents (owner_user_id, agent_ref, version, definition, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAgentForOwner :one
SELECT * FROM agents
WHERE owner_user_id = $1 AND agent_ref = $2 AND version = $3;

-- name: ListAgentsForOwner :many
SELECT * FROM agents
WHERE owner_user_id = $1
ORDER BY agent_ref, created_at DESC;

-- name: GetAgentDisplayForSubscriber :one
SELECT id, owner_user_id, agent_ref, version, display_meta, status, created_at
FROM agents
WHERE id = $1;

-- name: MarkAgentImmutable :exec
UPDATE agents SET immutable = true WHERE id = $1;

-- name: DeleteAgent :exec
DELETE FROM agents WHERE id = $1;
