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

-- name: ListConversationsForUserBundle :many
-- 独立聊天页（/chat/bundle/:bundleId）左侧的"最近对话"列表：按
-- session_id 把同一个人在同一个 Bundle 下的多次运行折成一段段对话，最近
-- 活跃的排最前。标题取这段对话里最早一次运行的 bundle.started 事件
-- payload.input.message（用户发的第一句原话）——这是运行时早就写好的
-- 事件，不用额外存一份标题。
-- 老运行没有 session_id（B1 迁移之前），本来就不归属任何一段"对话"，天
-- 然被 session_id IS NOT NULL 排除在外。
WITH sessions AS (
  SELECT
    br.session_id,
    -- run id 是随机十六进制串，MIN(id) 不代表"最早的那次运行"——要按
    -- created_at 排序取第一个，用 array_agg 而不是再一次相关子查询。
    (array_agg(br.id ORDER BY br.created_at ASC))[1] AS first_run_id,
    MIN(br.created_at)::timestamptz AS started_at,
    MAX(br.created_at)::timestamptz AS last_active_at,
    COUNT(*) AS run_count
  FROM bundle_runs br
  WHERE br.triggered_by = $1 AND br.bundle_id = $2 AND br.session_id IS NOT NULL
  GROUP BY br.session_id
)
SELECT
  s.session_id,
  s.started_at,
  s.last_active_at,
  s.run_count,
  -- COALESCE 到空串而不是留 NULL：万一某个 session 的首次运行还没落下
  -- bundle.started 事件（理论上不该发生，但标题这种展示字段没必要因为
  -- 一行脏数据让整个列表接口报错），前端拿到空串就照 threadMessages 的
  -- 老规矩显示"新的对话"。
  CAST(COALESCE((
    SELECT e.payload->'input'->>'message'
    FROM bundle_run_events e
    WHERE e.run_id = s.first_run_id AND e.type = 'bundle.started'
    ORDER BY e.id ASC LIMIT 1
  ), '') AS text) AS title
FROM sessions s
ORDER BY s.last_active_at DESC
LIMIT $3;
