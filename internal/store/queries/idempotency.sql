-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE key = $1 AND expires_at > now();

-- name: PutIdempotencyKey :exec
INSERT INTO idempotency_keys (key, owner_user_id, status_code, response_body, expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (key) DO NOTHING;

-- name: DeleteExpiredIdempotencyKeys :exec
DELETE FROM idempotency_keys WHERE expires_at <= now();
