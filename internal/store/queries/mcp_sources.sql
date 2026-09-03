-- name: CreateMCPSource :one
INSERT INTO mcp_sources (name, base_url, protocol, api_prefix, api_key_encrypted)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SetMCPSourceAPIKey :execrows
-- 单独换密钥：密钥过期时不必删掉源再重建（那会连审核结论一起丢掉）。
UPDATE mcp_sources SET api_key_encrypted = $2 WHERE id = $1;

-- name: ListMCPSources :many
-- 左联 market_mcp_servers 只为拿每个源的条目数，设置页直接展示。
SELECT s.*, COUNT(m.id)::bigint AS server_count
FROM mcp_sources s
LEFT JOIN market_mcp_servers m ON m.source_id = s.id
GROUP BY s.id
ORDER BY s.created_at DESC;

-- name: GetMCPSource :one
SELECT * FROM mcp_sources WHERE id = $1;

-- name: GetMCPSourceByURL :one
SELECT * FROM mcp_sources WHERE base_url = $1;

-- name: DeleteMCPSource :execrows
DELETE FROM mcp_sources WHERE id = $1;

-- name: MarkMCPSourceSynced :one
UPDATE mcp_sources
SET last_synced_at = now(), last_sync_error = NULL
WHERE id = $1
RETURNING *;

-- name: MarkMCPSourceSyncError :one
UPDATE mcp_sources
SET last_synced_at = now(), last_sync_error = $2
WHERE id = $1
RETURNING *;

-- name: UpsertMarketMCPServer :one
-- 同步落库：同一 (source, slug) 覆盖刷新，上次同步后被上游下架的条目由
-- Sync 清理（见 DeleteStaleMarketMCPServers）。
--
-- review_* 刻意不在 DO UPDATE 里：审核结论是本地判断，不是上游字段。每次
-- 同步都重置的话，管理员批准过的条目会在下次同步后集体打回待审。
INSERT INTO market_mcp_servers (source_id, slug, name, summary, version, license, repository_url, remote_url, remote_type, topics, updated_at, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (source_id, slug) DO UPDATE SET
    name           = EXCLUDED.name,
    summary        = EXCLUDED.summary,
    version        = EXCLUDED.version,
    license        = EXCLUDED.license,
    repository_url = EXCLUDED.repository_url,
    remote_url     = EXCLUDED.remote_url,
    remote_type    = EXCLUDED.remote_type,
    topics         = EXCLUDED.topics,
    updated_at     = EXCLUDED.updated_at,
    raw            = EXCLUDED.raw,
    synced_at      = now()
RETURNING *;

-- name: DeleteStaleMarketMCPServers :execrows
DELETE FROM market_mcp_servers
WHERE source_id = $1 AND slug <> ALL($2::text[]);

-- name: ListMarketMCPServers :many
-- MCP 管理 → 市场视图：所有启用源里**审核通过**的缓存条目。未过审的条目
-- 对普通用户根本不存在（审核台走 ListMarketMCPServersForReview）。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_mcp_servers m
JOIN mcp_sources s ON s.id = m.source_id AND s.status = 1
WHERE m.review_status = 'approved'
ORDER BY m.updated_at DESC NULLS LAST;

-- name: ListMarketMCPServersForReview :many
-- 审核台（系统配置 → MCP 源）：不筛源状态、不筛审核状态地列出同步条目。
-- 分页和搜索都在库里做——一个公开注册中心动辄上千条。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_mcp_servers m
JOIN mcp_sources s ON s.id = m.source_id
WHERE (sqlc.narg('review_status')::text IS NULL OR m.review_status = sqlc.narg('review_status')::text)
  AND (sqlc.narg('source_id')::bigint IS NULL OR m.source_id = sqlc.narg('source_id')::bigint)
  AND (sqlc.narg('search')::text IS NULL
       OR m.slug ILIKE '%' || sqlc.narg('search')::text || '%'
       OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR COALESCE(m.summary, '') ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY m.review_status = 'pending' DESC, m.synced_at DESC, m.slug
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountMarketMCPServersForReview :one
-- 与 ListMarketMCPServersForReview 同一套筛选条件下的总数，供前端算总页数。
SELECT COUNT(*)::bigint
FROM market_mcp_servers m
JOIN mcp_sources s ON s.id = m.source_id
WHERE (sqlc.narg('review_status')::text IS NULL OR m.review_status = sqlc.narg('review_status')::text)
  AND (sqlc.narg('source_id')::bigint IS NULL OR m.source_id = sqlc.narg('source_id')::bigint)
  AND (sqlc.narg('search')::text IS NULL
       OR m.slug ILIKE '%' || sqlc.narg('search')::text || '%'
       OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR COALESCE(m.summary, '') ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: CountMarketMCPServersByReview :many
-- 审核台顶部的状态计数。source_id 为空时统计全部源；审核台是按源进的，
-- 那里必须传源 ID，否则顶部计数和下面的列表对不上。
SELECT review_status, COUNT(*)::bigint AS count
FROM market_mcp_servers
WHERE (sqlc.narg('source_id')::bigint IS NULL OR source_id = sqlc.narg('source_id')::bigint)
GROUP BY review_status;

-- name: SetMarketMCPServerReview :execrows
UPDATE market_mcp_servers
SET review_status = $2, review_note = $3, reviewed_at = now(), reviewed_by = $4
WHERE id = $1;

-- name: GetMarketMCPServer :one
-- 对外一律用行 id 寻址：上游的限定名带点和斜杠，做不了 URL 路径参数。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_mcp_servers m
JOIN mcp_sources s ON s.id = m.source_id
WHERE m.id = $1;
