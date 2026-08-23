-- Dependency closure (spec-08) is always owner-scoped: a Bundle/Agent only
-- ever references resources in the SAME owner's space by ref, so every
-- "is X published" check below joins through the resource table's
-- owner_user_id rather than trusting listing_ref alone (listing_ref is
-- globally unique across all authors, like an npm package name — see
-- migration 0003's UNIQUE(listing_ref, version)).
--
-- distribution: 1 distributing / 2 stopped / 3 delisted (admin takedown).
-- "published" for dependency-satisfaction purposes means distribution != 3
-- — a stopped listing still satisfies dependents (spec-08 "无影响，继续可用"),
-- only an admin delisting actually removes it from the graph.

-- name: CreateListing :one
INSERT INTO marketplace_listings
    (author_user_id, resource_type, resource_id, listing_ref, version, changelog)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetListingByID :one
SELECT * FROM marketplace_listings WHERE id = $1;

-- name: GetListingByListingRefLatestPublished :one
SELECT * FROM marketplace_listings
WHERE listing_ref = $1 AND distribution != 3
ORDER BY published_at DESC
LIMIT 1;

-- name: GetListingByListingRefAndVersion :one
SELECT * FROM marketplace_listings
WHERE listing_ref = $1 AND version = $2 AND distribution != 3;

-- name: ListListingVersionHistory :many
SELECT version, changelog, published_at FROM marketplace_listings
WHERE listing_ref = $1 AND distribution != 3
ORDER BY published_at DESC;

-- name: SetListingDistribution :exec
UPDATE marketplace_listings SET distribution = $2 WHERE id = $1;

-- name: IncrementListingSubscriberCount :exec
UPDATE marketplace_listings SET subscriber_count = subscriber_count + 1 WHERE id = $1;

-- name: DecrementListingSubscriberCount :exec
UPDATE marketplace_listings SET subscriber_count = subscriber_count - 1 WHERE id = $1;

-- ── Dependency-satisfaction checks (publish-time closure walk) ──────────

-- name: GetAgentListingForOwnerByRefVersion :one
SELECT ml.* FROM marketplace_listings ml
JOIN agents a ON a.id = ml.resource_id
WHERE ml.resource_type = 'agent' AND a.owner_user_id = $1 AND a.agent_ref = $2 AND a.version = $3
  AND ml.distribution != 3;

-- name: GetBundleListingForOwnerByRefVersion :one
SELECT ml.* FROM marketplace_listings ml
JOIN bundles b ON b.id = ml.resource_id
WHERE ml.resource_type = 'bundle' AND b.owner_user_id = $1 AND b.bundle_ref = $2 AND b.version = $3
  AND ml.distribution != 3;

-- name: GetSkillListingForOwnerByRef :one
-- Skill/MCP refs in an Agent's capabilities are not version-pinned in the
-- DSL (unlike Bundle.agents[].version), so satisfaction only requires SOME
-- published version of the ref to exist — the most recently published one.
SELECT ml.* FROM marketplace_listings ml
JOIN skills s ON s.id = ml.resource_id
WHERE ml.resource_type = 'skill' AND s.owner_user_id = $1 AND s.ref = $2
  AND ml.distribution != 3
ORDER BY ml.published_at DESC
LIMIT 1;

-- name: GetMCPServerListingForOwnerByRef :one
SELECT ml.* FROM marketplace_listings ml
JOIN mcp_servers m ON m.id = ml.resource_id
WHERE ml.resource_type = 'mcp' AND m.owner_user_id = $1 AND m.ref = $2
  AND ml.distribution != 3
ORDER BY ml.published_at DESC
LIMIT 1;

-- ── Reverse-dependency checks (unpublish-time, 70007) ───────────────────

-- name: FindPublishedBundlesReferencingAgentRef :many
-- Which of the owner's *currently published* Bundles reference this Agent
-- ref (any version — used to block unpublishing the Agent entirely; the
-- exact-version variant used at publish-time doesn't apply here since any
-- version of the agent being gone would break the dependent Bundle).
SELECT b.bundle_ref, b.version FROM bundles b
JOIN marketplace_listings ml ON ml.resource_type = 'bundle' AND ml.resource_id = b.id
WHERE b.owner_user_id = $1
  AND ml.distribution != 3
  AND (b.definition->'agents') @> jsonb_build_array(jsonb_build_object('ref', sqlc.arg('agent_ref')::text));

-- name: FindPublishedAgentsReferencingToolRef :many
SELECT a.agent_ref, a.version FROM agents a
JOIN marketplace_listings ml ON ml.resource_type = 'agent' AND ml.resource_id = a.id
WHERE a.owner_user_id = $1
  AND ml.distribution != 3
  AND (a.definition->'capabilities'->'tools') ? sqlc.arg('tool_ref')::text;

-- name: FindPublishedAgentsReferencingSkillRef :many
SELECT a.agent_ref, a.version FROM agents a
JOIN marketplace_listings ml ON ml.resource_type = 'agent' AND ml.resource_id = a.id
WHERE a.owner_user_id = $1
  AND ml.distribution != 3
  AND (a.definition->'capabilities'->'skills') ? sqlc.arg('skill_ref')::text;

-- ── Browse / detail (blackbox: display_meta only, never definition) ────

-- name: ListPublishedAgentListingsPage :many
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       a.display_meta
FROM marketplace_listings ml
JOIN agents a ON a.id = ml.resource_id
WHERE ml.resource_type = 'agent' AND ml.distribution = 1 AND ml.id > $1
ORDER BY ml.id ASC
LIMIT $2;

-- name: ListPublishedBundleListingsPage :many
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       b.display_meta
FROM marketplace_listings ml
JOIN bundles b ON b.id = ml.resource_id
WHERE ml.resource_type = 'bundle' AND ml.distribution = 1 AND ml.id > $1
ORDER BY ml.id ASC
LIMIT $2;

-- name: ListPublishedSkillListingsPage :many
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       s.display_meta
FROM marketplace_listings ml
JOIN skills s ON s.id = ml.resource_id
WHERE ml.resource_type = 'skill' AND ml.distribution = 1 AND ml.id > $1
ORDER BY ml.id ASC
LIMIT $2;

-- name: ListPublishedMCPServerListingsPage :many
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       m.display_meta
FROM marketplace_listings ml
JOIN mcp_servers m ON m.id = ml.resource_id
WHERE ml.resource_type = 'mcp' AND ml.distribution = 1 AND ml.id > $1
ORDER BY ml.id ASC
LIMIT $2;

-- Constraints summary is extracted field-by-field via JSONB path operators
-- so the full `definition` blob never has to leave Postgres — the same
-- "DAO 层强制隔离" principle as the display-only list queries above, just
-- applied to a handful of numeric/string fields instead of a whole column.

-- name: GetAgentConstraintsSummaryByListingID :one
-- Every field of Agent.constraints is optional in the DSL (schema defaults
-- apply at runtime), so a raw ::int/::text cast on a possibly-absent key
-- would scan a Postgres NULL into a non-nullable Go int32/string and
-- panic. COALESCE to the DSL's own documented defaults (max_tool_calls 20,
-- timeout_seconds 300) keeps the value meaningful; estimated_tokens_range
-- has no default, so it's coalesced to '' and the API layer treats an
-- empty string as "not disclosed" rather than guessing a range.
SELECT
    (COALESCE((a.definition->'constraints'->>'max_tool_calls')::int, 20))::int AS max_tool_calls,
    (COALESCE((a.definition->'constraints'->>'timeout_seconds')::int, 300))::int AS timeout_seconds,
    (COALESCE(a.definition->'constraints'->>'estimated_tokens_range', ''))::text AS estimated_tokens_range
FROM marketplace_listings ml
JOIN agents a ON a.id = ml.resource_id
WHERE ml.id = $1;

-- name: GetBundleConstraintsSummaryByListingID :one
SELECT
    NULL::int AS max_tool_calls,
    (COALESCE((b.definition->'limits'->>'max_wall_clock_seconds')::int, 3600))::int AS timeout_seconds,
    NULL::text AS estimated_tokens_range
FROM marketplace_listings ml
JOIN bundles b ON b.id = ml.resource_id
WHERE ml.id = $1;

-- name: GetAgentListingDisplayByListingID :one
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       a.display_meta
FROM marketplace_listings ml
JOIN agents a ON a.id = ml.resource_id
WHERE ml.id = $1;

-- name: GetBundleListingDisplayByListingID :one
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       b.display_meta
FROM marketplace_listings ml
JOIN bundles b ON b.id = ml.resource_id
WHERE ml.id = $1;

-- name: GetSkillListingDisplayByListingID :one
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       s.display_meta
FROM marketplace_listings ml
JOIN skills s ON s.id = ml.resource_id
WHERE ml.id = $1;

-- name: GetMCPServerListingDisplayByListingID :one
SELECT ml.id, ml.listing_ref, ml.resource_type, ml.version, ml.visibility,
       ml.author_user_id, ml.subscriber_count, ml.run_count, ml.published_at,
       m.display_meta
FROM marketplace_listings ml
JOIN mcp_servers m ON m.id = ml.resource_id
WHERE ml.id = $1;

-- ── Subscriptions ────────────────────────────────────────────────────

-- name: CreateSubscription :one
INSERT INTO subscriptions (subscriber_id, listing_id, local_alias)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSubscriptionByListingAndSubscriber :one
SELECT * FROM subscriptions WHERE subscriber_id = $1 AND listing_id = $2;

-- name: GetSubscriptionForSubscriberByListingRef :one
-- Enforces "一个 ref 只保留一个生效订阅" (spec-08 已决事项): a subscriber can
-- have at most one active subscription per listing_ref, regardless of
-- which version it's bound to — switching versions is an explicit upgrade,
-- never a second subscribe.
SELECT s.* FROM subscriptions s
JOIN marketplace_listings ml ON ml.id = s.listing_id
WHERE s.subscriber_id = $1 AND ml.listing_ref = $2;

-- name: GetSubscriptionByIDForSubscriber :one
SELECT * FROM subscriptions WHERE id = $1 AND subscriber_id = $2;

-- name: ListSubscriptionsForUserPage :many
SELECT * FROM subscriptions
WHERE subscriber_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = $1 AND subscriber_id = $2;

-- name: UpdateSubscriptionListing :one
UPDATE subscriptions SET listing_id = $3 WHERE id = $1 AND subscriber_id = $2
RETURNING *;
