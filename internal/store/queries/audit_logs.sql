-- name: CreateAuditLog :one
INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, detail)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAuditLogsForTarget :many
SELECT * FROM audit_logs WHERE target_type = $1 AND target_id = $2 ORDER BY created_at ASC;
