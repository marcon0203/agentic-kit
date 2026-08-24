-- name: InsertKBChunk :one
INSERT INTO kb_chunks (knowledge_base_id, owner_user_id, source_ref, chunk_index, content, embedding)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: DeleteKBChunksBySource :exec
DELETE FROM kb_chunks WHERE knowledge_base_id = $1 AND owner_user_id = $2 AND source_ref = $3;

-- name: ListKBSources :many
SELECT source_ref, count(*)::int AS chunk_count, min(created_at)::timestamptz AS ingested_at
FROM kb_chunks
WHERE knowledge_base_id = $1 AND owner_user_id = $2
GROUP BY source_ref
ORDER BY min(created_at) DESC;

-- name: SearchKBChunks :many
SELECT id, source_ref, chunk_index, content, (embedding <=> $3)::float8 AS distance
FROM kb_chunks
WHERE knowledge_base_id = $1 AND owner_user_id = $2
ORDER BY embedding <=> $3
LIMIT $4;
