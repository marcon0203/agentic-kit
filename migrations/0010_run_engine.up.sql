-- spec-11: run lifecycle, human gates, and an append-only audit trail.

ALTER TABLE bundle_runs
    ADD COLUMN cancel_requested_at TIMESTAMPTZ;
    -- Set when a user calls POST /runs/{id}/cancel; the running goroutine
    -- observes this (via context cancellation, in-process) and stops
    -- within a few seconds without waiting for the current Agent step to
    -- finish. Already-produced output/events/usage are kept as-is.

CREATE TABLE human_gates (
    id              BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    run_id          VARCHAR(32)   NOT NULL REFERENCES bundle_runs(id),
    node            VARCHAR(64)   NOT NULL,
    status          VARCHAR(16)   NOT NULL DEFAULT 'pending',
        -- pending / approved / rejected / timeout / aborted
    timeout_seconds INT,
    on_timeout      VARCHAR(16)   NOT NULL DEFAULT 'abort',
    approver_roles  JSONB         NOT NULL DEFAULT '[]',
        -- Bundle.orchestration.human_gates[].approver_roles, carried over
        -- for display; V1 has no team/role system (架构文档 "V1 明确不做
        -- 多租户"), so the actual authorization check is always "only the
        -- run's own triggered_by user may resolve this gate" regardless
        -- of what this column says — see spec-11's "审批人角色二次校验".
    resolved_by     BIGINT REFERENCES users(id),
    comment         TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX idx_human_gates_run ON human_gates (run_id, id);
-- Backs the timeout-scanning job's poll (spec-11: "超时策略...由平台侧定时
-- 任务扫描待处理 gate 实现").
CREATE INDEX idx_human_gates_pending ON human_gates (created_at) WHERE status = 'pending';

CREATE TABLE audit_logs (
    id            BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    actor_user_id BIGINT REFERENCES users(id),
    action        VARCHAR(64) NOT NULL,
    target_type   VARCHAR(32) NOT NULL,
    target_id     VARCHAR(64) NOT NULL,
    detail        JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_target ON audit_logs (target_type, target_id);

-- Append-only (架构文档 "审计...audit_logs 表只追加"): once written, a row
-- can never be changed or removed, matching the "不可篡改" requirement for
-- human-gate approval records.
CREATE OR REPLACE FUNCTION reject_audit_log_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_logs 只追加，不可修改或删除';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_logs_reject_update
  BEFORE UPDATE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation();
CREATE TRIGGER audit_logs_reject_delete
  BEFORE DELETE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION reject_audit_log_mutation();
