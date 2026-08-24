-- name: InsertMemoryEntry :one
INSERT INTO memory_entries (memory_id, owner_user_id, app_name, agent_user_id, session_id, author, content)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: SearchMemoryEntries :many
SELECT id, author, content, created_at
FROM memory_entries
WHERE memory_id = $1 AND owner_user_id = $2 AND app_name = $3 AND agent_user_id = $4
  AND content_tsv @@ plainto_tsquery('simple', $5)
ORDER BY ts_rank(content_tsv, plainto_tsquery('simple', $5)) DESC
LIMIT $6;
