-- name: CreateListing :one
INSERT INTO marketplace_listings
    (author_user_id, resource_type, resource_id, listing_ref, version, changelog)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListListingsByType :many
SELECT * FROM marketplace_listings
WHERE resource_type = $1 AND distribution = 1
ORDER BY published_at DESC
LIMIT $2 OFFSET $3;

-- name: GetListingByRefVersion :one
SELECT * FROM marketplace_listings
WHERE listing_ref = $1 AND version = $2;

-- name: SetListingDistribution :exec
UPDATE marketplace_listings SET distribution = $2 WHERE id = $1;

-- name: IncrementListingSubscriberCount :exec
UPDATE marketplace_listings SET subscriber_count = subscriber_count + 1 WHERE id = $1;

-- name: DecrementListingSubscriberCount :exec
UPDATE marketplace_listings SET subscriber_count = subscriber_count - 1 WHERE id = $1;

-- name: CreateSubscription :one
INSERT INTO subscriptions (subscriber_id, listing_id, local_alias)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListSubscriptionsForUser :many
SELECT * FROM subscriptions
WHERE subscriber_id = $1
ORDER BY created_at DESC;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = $1 AND subscriber_id = $2;
