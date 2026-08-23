-- name: CreateReport :one
INSERT INTO reports (listing_id, reporter_user_id, reason)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListPendingReportsPage :many
SELECT * FROM reports WHERE status = 'pending' AND id < $1 ORDER BY id DESC LIMIT $2;

-- name: GetReportByID :one
SELECT * FROM reports WHERE id = $1;

-- name: ResolveReport :one
UPDATE reports SET status = 'resolved', resolution = $2, resolved_by = $3, resolved_at = now()
WHERE id = $1
RETURNING *;
