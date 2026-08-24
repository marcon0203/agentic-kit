-- name: CreateCatalogProvider :one
INSERT INTO catalog_providers (provider_key, display_name, icon, base_url)
VALUES ($1, $2, $3, $4)
RETURNING id, provider_key, display_name, icon, base_url, status, created_at;

-- name: ListCatalogProviders :many
SELECT id, provider_key, display_name, icon, base_url, status, created_at
FROM catalog_providers
ORDER BY created_at ASC;

-- name: GetCatalogProvider :one
SELECT id, provider_key, display_name, icon, base_url, status, created_at
FROM catalog_providers
WHERE id = $1;

-- name: SetCatalogProviderStatus :exec
UPDATE catalog_providers SET status = $2 WHERE id = $1;

-- name: DeleteCatalogProvider :exec
DELETE FROM catalog_providers WHERE id = $1;

-- name: CreateCatalogModel :one
INSERT INTO catalog_models (provider_id, model, display_name, description, modality, featured)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, provider_id, model, display_name, description, modality, featured, status, created_at;

-- name: ListCatalogModelsForProvider :many
SELECT id, provider_id, model, display_name, description, modality, featured, status, created_at
FROM catalog_models
WHERE provider_id = $1
ORDER BY created_at ASC;

-- name: SetCatalogModelStatus :exec
UPDATE catalog_models SET status = $2 WHERE id = $1;

-- name: DeleteCatalogModel :exec
DELETE FROM catalog_models WHERE id = $1;

-- name: ListCatalogModelsPublic :many
-- Joined read for 模型广场 (GET /model-catalog): only enabled models under
-- enabled providers, newest provider first so a freshly configured provider
-- surfaces near the top instead of at the bottom of an ORDER BY id.
SELECT
    cm.model, cm.display_name, cm.description, cm.modality, cm.featured,
    cp.provider_key, cp.display_name AS provider_display_name, cp.icon AS provider_icon
FROM catalog_models cm
JOIN catalog_providers cp ON cp.id = cm.provider_id
WHERE cm.status = 1 AND cp.status = 1
ORDER BY cp.created_at ASC, cm.created_at ASC;
