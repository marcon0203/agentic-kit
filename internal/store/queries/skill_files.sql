-- name: CreateSkillFile :exec
INSERT INTO skill_files (skill_id, owner_user_id, path, size_bytes, content_type)
VALUES ($1, $2, $3, $4, $5);

-- name: ListSkillFilesForSkill :many
SELECT * FROM skill_files WHERE skill_id = $1 AND owner_user_id = $2 ORDER BY path ASC;
