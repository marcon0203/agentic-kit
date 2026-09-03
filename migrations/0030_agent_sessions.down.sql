DROP INDEX IF EXISTS idx_bundle_runs_session;
ALTER TABLE bundle_runs DROP COLUMN IF EXISTS session_id;
DROP TABLE IF EXISTS adk_user_state;
DROP TABLE IF EXISTS adk_app_state;
DROP TABLE IF EXISTS adk_session_events;
DROP TABLE IF EXISTS adk_sessions;
