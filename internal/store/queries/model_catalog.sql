-- name: CreateCatalogProvider :one
INSERT INTO catalog_providers (provider_key, display_name, icon, base_url)
VALUES ($1, $2, $3, $4)
RETURNING id, provider_key, display_name, icon, base_url, status, created_at, (default_api_key_encrypted IS NOT NULL)::bool AS has_credential;

-- name: ListCatalogProviders :many
SELECT id, provider_key, display_name, icon, base_url, status, created_at, (default_api_key_encrypted IS NOT NULL)::bool AS has_credential
FROM catalog_providers
ORDER BY created_at ASC;

-- name: GetCatalogProvider :one
SELECT id, provider_key, display_name, icon, base_url, status, created_at, (default_api_key_encrypted IS NOT NULL)::bool AS has_credential
FROM catalog_providers
WHERE id = $1;

-- name: SetCatalogProviderStatus :exec
UPDATE catalog_providers SET status = $2 WHERE id = $1;

-- name: SetCatalogProviderCredential :exec
-- encrypted_key is a nullable param: pass NULL to leave the stored key
-- untouched (admin only changed base_url, left the api_key field blank in
-- the edit dialog because it's already set), or an encrypted value to
-- replace it.
UPDATE catalog_providers
SET base_url = $2,
    default_api_key_encrypted = COALESCE(sqlc.narg('encrypted_key'), default_api_key_encrypted)
WHERE id = $1;

-- name: ListCatalogProviderDefaultCredentials :many
-- Admin-set org-wide fallback credentials, for ProviderKeyStore.Keys to
-- merge in when a user has no personal credential for that provider.
SELECT provider_key, base_url, default_api_key_encrypted
FROM catalog_providers
WHERE status = 1 AND default_api_key_encrypted IS NOT NULL;

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
