-- name: CreateResource :one
INSERT INTO resources (owner_user_id, type, ref, version, config, display_meta)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetResourceForOwner :one
SELECT * FROM resources
WHERE owner_user_id = $1 AND type = $2 AND ref = $3 AND version = $4;

-- name: ListResourcesForOwner :many
SELECT * FROM resources
WHERE owner_user_id = $1
ORDER BY type, ref, created_at DESC;

-- name: GetResourceDisplayForSubscriber :one
SELECT id, owner_user_id, type, ref, version, display_meta, status, created_at
FROM resources
WHERE id = $1;

-- name: SetResourceStatus :exec
UPDATE resources SET status = $2 WHERE id = $1;

-- name: MarkResourceImmutable :exec
UPDATE resources SET immutable = true WHERE id = $1;

-- name: DeleteResource :exec
DELETE FROM resources WHERE id = $1;
