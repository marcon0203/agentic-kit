package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// GateTimeoutScanInterval is how often RunGateTimeoutScanner polls for
// gates past their deadline.
const GateTimeoutScanInterval = 10 * time.Second

// RunGateTimeoutScanner implements spec-11's "超时策略...由平台侧定时任务
// 扫描待处理 gate 实现，超时后主动向 ADK 注入对应结果" — it never blocks a
// Wait() call itself; it just resolves the gateRegistry channel (and the
// DB row) the same way an explicit approval would, so the run's own
// goroutine unblocks through the exact same path either way.
func RunGateTimeoutScanner(ctx context.Context, q store.Querier, gates *gateRegistry, logger *slog.Logger) {
	ticker := time.NewTicker(GateTimeoutScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanOnce(ctx, q, gates, logger)
		}
	}
}

func scanOnce(ctx context.Context, q store.Querier, gates *gateRegistry, logger *slog.Logger) {
	pending, err := q.ListPendingHumanGatesPastTimeout(ctx)
	if err != nil {
		if logger != nil {
			logger.Error("gate_timeout_scan_failed", "error", err)
		}
		return
	}
	for _, gate := range pending {
		resolveTimedOutGate(ctx, q, gates, gate, logger)
	}
}

func resolveTimedOutGate(ctx context.Context, q store.Querier, gates *gateRegistry, gate store.HumanGate, logger *slog.Logger) {
	var status string
	var approved bool
	switch gate.OnTimeout {
	case "auto_approve":
		status, approved = "approved", true
	case "auto_reject":
		status, approved = "rejected", false
	default: // "abort"
		status, approved = "timeout", false
	}

	updated, err := q.ResolveHumanGate(ctx, store.ResolveHumanGateParams{
		ID: gate.ID, Status: status, ResolvedBy: pgtype.Int8{}, Comment: pgtype.Text{String: "timed out: " + gate.OnTimeout, Valid: true},
	})
	if err != nil {
		// Already resolved by a human in the meantime — nothing to do.
		return
	}

	detail, _ := json.Marshal(map[string]any{"run_id": gate.RunID, "node": gate.Node, "on_timeout": gate.OnTimeout})
	if _, err := q.CreateAuditLog(ctx, store.CreateAuditLogParams{
		ActorUserID: pgtype.Int8{}, Action: "human_gate." + status, TargetType: "human_gate",
		TargetID: strconv.FormatInt(updated.ID, 10), Detail: detail,
	}); err != nil && logger != nil {
		logger.Error("gate_timeout_audit_log_failed", "gate_id", gate.ID, "error", err)
	}

	resolvedPayload, _ := json.Marshal(map[string]any{"gate_id": gate.ID, "status": status, "on_timeout": gate.OnTimeout})
	if _, err := q.InsertBundleRunEvent(ctx, store.InsertBundleRunEventParams{
		RunID: gate.RunID, Type: "human_gate.resolved", Node: pgtype.Text{String: gate.Node, Valid: true},
		Payload: resolvedPayload, IsInternal: false,
	}); err != nil && logger != nil {
		logger.Error("gate_timeout_event_failed", "gate_id", gate.ID, "error", err)
	}

	gates.resolve(gate.ID, gateResolution{approved: approved, reason: "human gate timed out (" + gate.OnTimeout + ")"})
}
