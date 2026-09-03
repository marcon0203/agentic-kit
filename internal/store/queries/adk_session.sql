-- ADK 会话持久化。见 migrations/0030_agent_sessions.up.sql 的表注释。

-- name: UpsertADKSession :one
-- 建会话。已存在就原样返回——ADK 的 AutoCreateSession 每轮都会尝试建一
-- 次，冲突是常态而不是错误。
INSERT INTO adk_sessions (app_name, user_id, session_id, state)
VALUES ($1, $2, $3, $4)
ON CONFLICT (app_name, user_id, session_id) DO UPDATE SET app_name = EXCLUDED.app_name
RETURNING app_name, user_id, session_id, state, created_at, updated_at;

-- name: GetADKSession :one
SELECT app_name, user_id, session_id, state, created_at, updated_at
FROM adk_sessions
WHERE app_name = $1 AND user_id = $2 AND session_id = $3;

-- name: ListADKSessions :many
-- 只列会话本身，不带事件：ADK 的 List 语义就是"会话清单"，把每段对话的
-- 全部事件都拉出来会让一次列表变成全表扫描。
SELECT app_name, user_id, session_id, state, created_at, updated_at
FROM adk_sessions
WHERE app_name = $1 AND user_id = $2
ORDER BY updated_at DESC;

-- name: DeleteADKSession :exec
DELETE FROM adk_sessions WHERE app_name = $1 AND user_id = $2 AND session_id = $3;

-- name: ListADKSessionEvents :many
SELECT seq, event_id, author, event, created_at
FROM adk_session_events
WHERE app_name = $1 AND user_id = $2 AND session_id = $3
ORDER BY seq ASC;

-- name: AppendADKSessionEvent :exec
INSERT INTO adk_session_events (app_name, user_id, session_id, event_id, author, event)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: SetADKSessionState :exec
UPDATE adk_sessions SET state = $4, updated_at = now()
WHERE app_name = $1 AND user_id = $2 AND session_id = $3;

-- name: GetADKAppState :one
SELECT state FROM adk_app_state WHERE app_name = $1;

-- name: MergeADKAppState :exec
-- || 是 jsonb 的浅合并，右侧覆盖左侧——正好是 ADK state delta 的语义。
INSERT INTO adk_app_state (app_name, state) VALUES ($1, $2)
ON CONFLICT (app_name) DO UPDATE
SET state = adk_app_state.state || EXCLUDED.state, updated_at = now();

-- name: GetADKUserState :one
SELECT state FROM adk_user_state WHERE app_name = $1 AND user_id = $2;

-- name: MergeADKUserState :exec
INSERT INTO adk_user_state (app_name, user_id, state) VALUES ($1, $2, $3)
ON CONFLICT (app_name, user_id) DO UPDATE
SET state = adk_user_state.state || EXCLUDED.state, updated_at = now();
