-- name: CreateBundleRun :one
INSERT INTO bundle_runs (id, bundle_id, triggered_by, via_listing_id, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetBundleRun :one
SELECT * FROM bundle_runs WHERE id = $1;

-- name: ListBundleRunsForUser :many
SELECT * FROM bundle_runs
WHERE triggered_by = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListBundleRunsByBundleAndStatus :many
SELECT * FROM bundle_runs
WHERE bundle_id = $1 AND status = $2
ORDER BY created_at DESC;

-- name: UpdateBundleRunStatus :exec
UPDATE bundle_runs SET status = $2, error = $3, finished_at = $4 WHERE id = $1;

-- name: UpdateBundleRunUsage :exec
UPDATE bundle_runs SET total_tokens = total_tokens + $2, cost_usd = cost_usd + $3 WHERE id = $1;

-- name: InsertBundleRunEvent :one
INSERT INTO bundle_run_events (run_id, type, node, payload, is_internal)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListBundleRunEventsAfter :many
SELECT * FROM bundle_run_events
WHERE run_id = $1 AND id > $2
ORDER BY id ASC;

-- name: ListBundleRunEventsAfterExternal :many
SELECT * FROM bundle_run_events
WHERE run_id = $1 AND id > $2 AND is_internal = false
ORDER BY id ASC;
