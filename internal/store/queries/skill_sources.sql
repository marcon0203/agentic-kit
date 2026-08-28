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
INSERT INTO market_skills (source_id, slug, name, summary, version, license, changelog, topics, stars, downloads, updated_at, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (source_id, slug) DO UPDATE SET
    name        = EXCLUDED.name,
    summary     = EXCLUDED.summary,
    version     = EXCLUDED.version,
    license     = EXCLUDED.license,
    changelog   = EXCLUDED.changelog,
    topics      = EXCLUDED.topics,
    stars       = EXCLUDED.stars,
    downloads   = EXCLUDED.downloads,
    updated_at  = EXCLUDED.updated_at,
    raw         = EXCLUDED.raw,
    synced_at   = now()
RETURNING *;

-- name: DeleteStaleMarketSkills :execrows
DELETE FROM market_skills
WHERE source_id = $1 AND slug <> ALL($2::text[]);

-- name: ListMarketSkills :many
-- Skill 管理 → 市场视图：所有启用源的缓存条目，附源信息供卡片和详情回链。
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id AND s.status = 1
ORDER BY m.updated_at DESC NULLS LAST;

-- name: GetMarketSkill :one
SELECT m.*, s.name AS source_name, s.base_url AS source_base_url
FROM market_skills m
JOIN skill_sources s ON s.id = m.source_id
WHERE m.source_id = $1 AND m.slug = $2;
