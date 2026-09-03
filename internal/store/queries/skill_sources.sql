-- name: CreateSkillSource :one
INSERT INTO skill_sources (name, base_url)
VALUES ($1, $2)
RETURNING *;

-- name: ListSkillSources :many
-- 左联 market_skills 只为拿每个源的条目数，设置页直接展示。
SELECT s.*, COUNT(m.id)::bigint AS skill_count
FROM skill_sources s
LEFT JOIN market_skills m ON m.source_id = s.id
GROUP BY s.id
ORDER BY s.created_at DESC;

-- name: GetSkillSource :one
SELECT * FROM skill_sources WHERE id = $1;

-- name: GetSkillSourceByURL :one
SELECT * FROM skill_sources WHERE base_url = $1;

-- name: DeleteSkillSource :execrows
DELETE FROM skill_sources WHERE id = $1;

-- name: MarkSkillSourceSynced :one
UPDATE skill_sources
SET last_synced_at = now(), last_sync_error = NULL
WHERE id = $1
RETURNING *;

-- name: MarkSkillSourceSyncError :one
UPDATE skill_sources
SET last_synced_at = now(), last_sync_error = $2
WHERE id = $1
RETURNING *;

-- name: UpsertMarketSkill :one
-- 同步落库：同一 (source, slug) 覆盖刷新，上次同步后被上游下架的条目由
-- Sync 清理（见 DeleteStaleMarketSkills）。
--
-- review_status/review_note/reviewed_* 刻意不在 DO UPDATE 里：审核结论是
-- 本地的判断，不是上游字段。每次同步都重置的话，管理员批准过的条目会在下
-- 次同步后集体打回待审，等于审核白做。
INSERT INTO market_skills (source_id, slug, name, summary, version, license, changelog, topics, stars, downloads, updated_at, raw, icon_url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (source_id, slug) DO UPDATE SET
    name        = EXCLUDED.name,
    summary     = EXCLUDED.summary,
    version     = EXCLUDED.version,
    license     = EXCLUDED.license,
    changelog   = EXCLUDED.changelog,
    topics      = EXCLUDED.topics,
    stars       = EXCLUDED.stars,
    downloads   = EXCLUDED.downloads,
    icon_url    = EXCLUDED.icon_url,
    updated_at  = EXCLUDED.updated_at,
    raw         = EXCLUDED.raw,
    synced_at   = now()
RETURNING *;

-- name: DeleteStaleMarketSkills :execrows
DELETE FROM market_skills
WHERE source_id = $1 AND slug <> ALL($2::text[]);

-- name: ListMarketSkills :many
-- Skill 管理 → 市场视图：所有启用源里**审核通过**的缓存条目，附源信息供
-- 卡片和详情回链。未过审的条目对普通用户根本不存在（审核台走
-- ListMarketSkillsForReview）。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id AND s.status = 1
WHERE m.review_status = 'approved'
ORDER BY m.updated_at DESC NULLS LAST;

-- name: ListMarketSkillsForReview :many
-- 审核台（系统配置 → Skill 源）：不筛源状态、不筛审核状态地列出同步条目，
-- 让管理员看得到"同步进来了什么"。sqlc.narg 为空时该条件不生效。
--
-- 分页在这里做而不是前端切片：一个公开源同步下来动辄成百上千条，全量返回
-- 一次要拖着整张表过网络。搜索同理——只筛当前页等于没筛。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id
WHERE (sqlc.narg('review_status')::text IS NULL OR m.review_status = sqlc.narg('review_status')::text)
  AND (sqlc.narg('source_id')::bigint IS NULL OR m.source_id = sqlc.narg('source_id')::bigint)
  AND (sqlc.narg('search')::text IS NULL
       OR m.slug ILIKE '%' || sqlc.narg('search')::text || '%'
       OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR COALESCE(m.summary, '') ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY m.review_status = 'pending' DESC, m.synced_at DESC, m.slug
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountMarketSkillsForReview :one
-- 与 ListMarketSkillsForReview 同一套筛选条件下的总数，供前端算总页数。
SELECT COUNT(*)::bigint
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id
WHERE (sqlc.narg('review_status')::text IS NULL OR m.review_status = sqlc.narg('review_status')::text)
  AND (sqlc.narg('source_id')::bigint IS NULL OR m.source_id = sqlc.narg('source_id')::bigint)
  AND (sqlc.narg('search')::text IS NULL
       OR m.slug ILIKE '%' || sqlc.narg('search')::text || '%'
       OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR COALESCE(m.summary, '') ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: CountMarketSkillsByReview :many
-- 审核台顶部的状态计数，一次查完免得前端按状态各拉一遍。source_id 为空时
-- 统计全部源；审核台是按源进的，那里必须传源 ID，否则顶部计数和下面的列表
-- 对不上。
SELECT review_status, COUNT(*)::bigint AS count
FROM market_skills
WHERE (sqlc.narg('source_id')::bigint IS NULL OR source_id = sqlc.narg('source_id')::bigint)
GROUP BY review_status;

-- name: SetMarketSkillReview :execrows
UPDATE market_skills
SET review_status = $3, review_note = $4, reviewed_at = now(), reviewed_by = $5
WHERE source_id = $1 AND slug = $2;

-- name: GetMarketSkill :one
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id
WHERE m.source_id = $1 AND m.slug = $2;
