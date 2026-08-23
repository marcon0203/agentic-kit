DROP TRIGGER IF EXISTS audit_logs_reject_delete ON audit_logs;
DROP TRIGGER IF EXISTS audit_logs_reject_update ON audit_logs;
DROP FUNCTION IF EXISTS reject_audit_log_mutation();
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS human_gates;
ALTER TABLE bundle_runs DROP COLUMN IF EXISTS cancel_requested_at;
