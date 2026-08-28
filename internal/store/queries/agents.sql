-- Owner-view and subscriber-view queries are kept physically separate so the
-- subscriber path can never accidentally SELECT `definition` (see
-- docs/架构设计文档_AI-Agent平台_V1.md 五、"黑盒发布的实现要点").

-- name: CreateAgent :one
INSERT INTO agents (owner_user_id, agent_ref, version, definition, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAgentByID :one
-- 按 version id 取一行（编辑页 / PATCH / DELETE 的入口），不经过 agent_ref。
SELECT * FROM agents WHERE id = $1 AND owner_user_id = $2;

-- name: GetAgentForOwner :one
SELECT * FROM agents
WHERE owner_user_id = $1 AND agent_ref = $2 AND version = $3;

-- name: ListAgentsForOwner :many
SELECT * FROM agents
WHERE owner_user_id = $1
ORDER BY agent_ref, created_at DESC;

-- name: ListAgentsForOwnerLatestPage :many
-- One row per agent_ref (its most recently created version), per
-- api/openapi.yaml's "每个 ref 返回最新启用版本" list contract.
SELECT DISTINCT ON (agent_ref) *
FROM agents
WHERE owner_user_id = $1 AND agent_ref > $2
ORDER BY agent_ref ASC, created_at DESC
LIMIT $3;

-- name: ListAgentVersionsForOwner :many
SELECT * FROM agents
WHERE owner_user_id = $1 AND agent_ref = $2
ORDER BY created_at DESC;

-- name: GetAgentLatestByRef :one
SELECT * FROM agents
WHERE owner_user_id = $1 AND agent_ref = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: CountActiveSubscribedListingsForAgentRef :one
SELECT count(*) FROM marketplace_listings ml
JOIN agents a ON a.id = ml.resource_id
WHERE ml.resource_type = 'agent' AND a.owner_user_id = $1 AND a.agent_ref = $2 AND ml.subscriber_count > 0;

-- name: FindBundlesReferencingAgentRef :many
SELECT bundle_ref, version FROM bundles
WHERE owner_user_id = $1
  AND definition->'agents' @> jsonb_build_array(jsonb_build_object('ref', $2::text));

-- name: DeleteAgentsByRef :exec
DELETE FROM agents WHERE owner_user_id = $1 AND agent_ref = $2;

-- name: GetAgentDisplayForSubscriber :one
SELECT id, owner_user_id, agent_ref, version, display_meta, status, created_at
FROM agents
WHERE id = $1;

-- name: MarkAgentImmutable :exec
UPDATE agents SET immutable = true WHERE id = $1;

-- name: SetAgentDisplayMeta :exec
UPDATE agents SET display_meta = $2 WHERE id = $1;

-- name: DeleteAgent :exec
DELETE FROM agents WHERE id = $1;

-- name: SetAgentStatusByID :exec
UPDATE agents SET status = $2 WHERE id = $1;
