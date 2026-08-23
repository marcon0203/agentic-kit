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

-- name: ListBundlesForOwnerLatestPage :many
-- One row per bundle_ref (its most recently created version).
SELECT DISTINCT ON (bundle_ref) *
FROM bundles
WHERE owner_user_id = $1 AND bundle_ref > $2
ORDER BY bundle_ref ASC, created_at DESC
LIMIT $3;

-- name: ListBundleVersionsForOwner :many
SELECT * FROM bundles
WHERE owner_user_id = $1 AND bundle_ref = $2
ORDER BY created_at DESC;

-- name: CountActiveSubscribedListingsForBundleRef :one
SELECT count(*) FROM marketplace_listings ml
JOIN bundles b ON b.id = ml.resource_id
WHERE ml.resource_type = 'bundle' AND b.owner_user_id = $1 AND b.bundle_ref = $2 AND ml.subscriber_count > 0;

-- name: DeleteBundlesByRef :exec
DELETE FROM bundles WHERE owner_user_id = $1 AND bundle_ref = $2;

-- name: GetBundleDisplayForSubscriber :one
SELECT id, owner_user_id, bundle_ref, version, display_meta, status, created_at
FROM bundles
WHERE id = $1;

-- name: MarkBundleImmutable :exec
UPDATE bundles SET immutable = true WHERE id = $1;

-- name: DeleteBundle :exec
DELETE FROM bundles WHERE id = $1;
