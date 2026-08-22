-- name: CreateBundle :one
INSERT INTO bundles (owner_user_id, bundle_ref, version, definition, display_meta)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetBundleForOwner :one
SELECT * FROM bundles
WHERE owner_user_id = $1 AND bundle_ref = $2 AND version = $3;

-- name: ListBundlesForOwner :many
SELECT * FROM bundles
WHERE owner_user_id = $1
ORDER BY bundle_ref, created_at DESC;

-- name: GetBundleDisplayForSubscriber :one
SELECT id, owner_user_id, bundle_ref, version, display_meta, status, created_at
FROM bundles
WHERE id = $1;

-- name: MarkBundleImmutable :exec
UPDATE bundles SET immutable = true WHERE id = $1;

-- name: DeleteBundle :exec
DELETE FROM bundles WHERE id = $1;
