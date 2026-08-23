-- name: CreateKnowledgeBase :one
INSERT INTO knowledge_bases (owner_user_id, ref, version, config, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetKnowledgeBaseByIDForOwner :one
SELECT * FROM knowledge_bases WHERE id = $1 AND owner_user_id = $2;

-- name: ListKnowledgeBasesForOwnerPage :many
SELECT * FROM knowledge_bases WHERE owner_user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3;

-- name: UpdateKnowledgeBase :one
UPDATE knowledge_bases SET display_meta = $3, config = $4, status = $5
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: MarkKnowledgeBaseImmutable :exec
UPDATE knowledge_bases SET immutable = true WHERE id = $1;

-- name: FindAgentsReferencingKnowledgeBaseRef :many
-- Knowledge bases surface to an Agent as an entry in capabilities.tools
-- (the DSL has no separate knowledge_base array) — same lookup as tools.
SELECT owner_user_id, agent_ref, version FROM agents
WHERE owner_user_id = $1
  AND (definition->'capabilities'->'tools') ? $2::text;

-- name: GetKnowledgeBaseLatestStatusByRef :one
SELECT status FROM knowledge_bases WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;
