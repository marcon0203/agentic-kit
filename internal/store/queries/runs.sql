-- name: CreateBundleRun :one
INSERT INTO bundle_runs (id, bundle_id, triggered_by, via_listing_id, status, session_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBundleRun :one
SELECT * FROM bundle_runs WHERE id = $1;

-- name: ListBundleRunsForUser :many
SELECT * FROM bundle_runs
WHERE triggered_by = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListBundleRunsForUserFiltered :many
-- bundle_ref/status filters are optional: pass '' to mean "no filter" on
-- that column (sqlc can't express NULL-able string params cleanly here,
-- and callers already have the value-or-empty from query params). Paginated
-- by offset, matching ListBundleRunsForUser's existing convention — run ids
-- are random, not monotonically sortable, so they can't back a keyset cursor.
SELECT br.*, b.bundle_ref AS bundle_ref, b.version AS bundle_version FROM bundle_runs br
JOIN bundles b ON b.id = br.bundle_id
WHERE br.triggered_by = sqlc.arg('triggered_by')
  AND (sqlc.arg('bundle_ref')::text = '' OR b.bundle_ref = sqlc.arg('bundle_ref'))
  AND (sqlc.arg('run_status')::text = '' OR br.status = sqlc.arg('run_status'))
ORDER BY br.created_at DESC
LIMIT sqlc.arg('page_limit') OFFSET sqlc.arg('page_offset');

-- name: MarkBundleRunCancelRequested :exec
UPDATE bundle_runs SET cancel_requested_at = now() WHERE id = $1 AND status = 'running';

-- name: ListBundleRunsByBundleAndStatus :many
SELECT * FROM bundle_runs
WHERE bundle_id = $1 AND status = $2
ORDER BY created_at DESC;

-- name: UpdateBundleRunStatus :exec
UPDATE bundle_runs SET status = $2, error = $3, finished_at = $4 WHERE id = $1;

-- name: UpdateBundleRunUsage :exec
UPDATE bundle_runs SET total_tokens = total_tokens + $2, cost_usd = cost_usd + $3 WHERE id = $1;

-- ── Usage & cost (/usage/me) ─────────────────────────────────────────
-- Usage is always scoped to the person who triggered the run — spec-09:
-- "黑盒资源的用量算订阅者的" — a subscriber running someone else's
-- published Bundle is the one whose usage this counts against, which
-- `triggered_by` already captures regardless of `via_listing_id`.

-- name: GetUsageSummaryForUser :one
SELECT
    COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens,
    COALESCE(SUM(cost_usd), 0)::numeric AS total_cost_usd,
    COUNT(*)::bigint AS run_count
FROM bundle_runs
WHERE triggered_by = $1 AND created_at >= $2;

-- name: GetUsageBreakdownByBundleForUser :many
SELECT
    b.bundle_ref AS key,
    COALESCE(SUM(br.total_tokens), 0)::bigint AS tokens,
    COALESCE(SUM(br.cost_usd), 0)::numeric AS cost_usd,
    COUNT(*)::bigint AS run_count
FROM bundle_runs br
JOIN bundles b ON b.id = br.bundle_id
WHERE br.triggered_by = $1 AND br.created_at >= $2
GROUP BY b.bundle_ref
ORDER BY tokens DESC;

-- name: GetUsageBreakdownByDayForUser :many
SELECT
    to_char(br.created_at, 'YYYY-MM-DD') AS key,
    COALESCE(SUM(br.total_tokens), 0)::bigint AS tokens,
    COALESCE(SUM(br.cost_usd), 0)::numeric AS cost_usd,
    COUNT(*)::bigint AS run_count
FROM bundle_runs br
WHERE br.triggered_by = $1 AND br.created_at >= $2
GROUP BY to_char(br.created_at, 'YYYY-MM-DD')
ORDER BY key DESC;

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

-- name: ListBundleRunsInSession :many
-- 一段对话里的全部运行，按时间正序——前端刷新页面后靠它把整段对话重建
-- 出来。会话按 (triggered_by, session_id) 定位，猜到别人的 session_id 也
-- 读不到别人的对话。
SELECT br.*, b.bundle_ref AS bundle_ref, b.version AS bundle_version
FROM bundle_runs br
JOIN bundles b ON b.id = br.bundle_id
WHERE br.triggered_by = $1 AND br.session_id = $2
ORDER BY br.created_at ASC;
