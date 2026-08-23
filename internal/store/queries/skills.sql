-- name: CreateSkill :one
INSERT INTO skills (owner_user_id, ref, version, config, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSkillByIDForOwner :one
SELECT * FROM skills WHERE id = $1 AND owner_user_id = $2;

-- name: ListSkillsForOwnerPage :many
SELECT * FROM skills WHERE owner_user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3;

-- name: UpdateSkill :one
UPDATE skills SET display_meta = $3, config = $4, status = $5
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: MarkSkillImmutable :exec
UPDATE skills SET immutable = true WHERE id = $1;

-- name: FindAgentsReferencingSkillRef :many
SELECT owner_user_id, agent_ref, version FROM agents
WHERE owner_user_id = $1
  AND (definition->'capabilities'->'skills') ? $2::text;

-- name: GetSkillLatestStatusByRef :one
SELECT status FROM skills WHERE owner_user_id = $1 AND ref = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetSkillByRefVersionForOwner :one
SELECT * FROM skills WHERE owner_user_id = $1 AND ref = $2 AND version = $3;

-- name: SetSkillDisplayMeta :exec
UPDATE skills SET display_meta = $2 WHERE id = $1;
