package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// GateTimeoutScanInterval is how often RunGateTimeoutScanner sweeps for
// gates past their deadline.
const GateTimeoutScanInterval = 10 * time.Second

// RunGateTimeoutScanner implements spec-11's "超时策略...由平台侧定时任务扫描
// 待处理 gate 实现，超时后主动向 ADK 注入对应结果".
//
// It never blocks a waiting run itself; it applies each gate's on_timeout
// policy through the same service call an explicit approval takes, so a
// run unblocks by exactly one path either way.
func RunGateTimeoutScanner(ctx context.Context, svc *run.Service, logger *slog.Logger) {
	ticker := time.NewTicker(GateTimeoutScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := svc.ResolveTimedOutGates(ctx); err != nil && logger != nil {
				logger.Error("gate_timeout_scan_failed", "error", err)
			}
		}
	}
}
