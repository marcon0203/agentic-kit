-- name: CreateHumanGate :one
INSERT INTO human_gates (run_id, node, timeout_seconds, on_timeout, approver_roles)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetHumanGateByID :one
SELECT * FROM human_gates WHERE id = $1;

-- name: GetPendingHumanGateForRunNode :one
-- A node can gate more than once across a Bundle's lifetime (e.g. inside a
-- self-loop retry), so this always resolves to the most recent occurrence
-- for that run+node — the one actually blocking right now.
SELECT * FROM human_gates
WHERE run_id = $1 AND node = $2 AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1;

-- name: ResolveHumanGate :one
UPDATE human_gates
SET status = $2, resolved_by = $3, comment = $4, resolved_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: ListPendingHumanGatesPastTimeout :many
-- Backs the timeout-scanning job (spec-11): a pending gate with a
-- timeout_seconds set, whose deadline has passed.
SELECT * FROM human_gates
WHERE status = 'pending'
  AND timeout_seconds IS NOT NULL
  AND created_at + make_interval(secs => timeout_seconds) < now()
ORDER BY created_at ASC;

-- name: ListHumanGatesForRun :many
SELECT * FROM human_gates WHERE run_id = $1 ORDER BY created_at ASC;
