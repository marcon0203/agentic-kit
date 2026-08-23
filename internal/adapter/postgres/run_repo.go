package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RunRepository implements run.Repository.
type RunRepository struct{ q store.Querier }

func NewRunRepository(q store.Querier) *RunRepository { return &RunRepository{q: q} }

func (r *RunRepository) Create(ctx context.Context, in run.Run) (run.Run, error) {
	var viaListingID pgtype.Int8
	if in.ViaListingID != nil {
		viaListingID = pgtype.Int8{Valid: true, Int64: *in.ViaListingID}
	}
	row, err := r.q.CreateBundleRun(ctx, store.CreateBundleRunParams{
		ID: in.ID, BundleID: in.BundleID, TriggeredBy: in.TriggeredBy,
		ViaListingID: viaListingID, Status: string(in.Status),
	})
	if err != nil {
		return run.Run{}, err
	}
	out := toDomainRun(row)
	// bundle_runs stores only the bundle id; ref and version came from the
	// resolution that produced this run, so carry them back out.
	out.BundleRef, out.BundleVersion = in.BundleRef, in.BundleVersion
	return out, nil
}

func (r *RunRepository) Get(ctx context.Context, runID string) (run.Run, error) {
	row, err := r.q.GetBundleRun(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNotFound
	}
	if err != nil {
		return run.Run{}, err
	}
	return toDomainRun(row), nil
}

func (r *RunRepository) ListPage(ctx context.Context, q run.ListQuery) ([]run.Run, error) {
	rows, err := r.q.ListBundleRunsForUserFiltered(ctx, store.ListBundleRunsForUserFilteredParams{
		TriggeredBy: q.TriggeredBy, BundleRef: q.BundleRef, RunStatus: q.Status,
		PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]run.Run, 0, len(rows))
	for _, row := range rows {
		item := toDomainRun(store.BundleRun{
			ID: row.ID, BundleID: row.BundleID, TriggeredBy: row.TriggeredBy, ViaListingID: row.ViaListingID,
			Status: row.Status, Error: row.Error, SharedState: row.SharedState, TotalTokens: row.TotalTokens,
			CostUsd: row.CostUsd, CreatedAt: row.CreatedAt, FinishedAt: row.FinishedAt,
		})
		item.BundleRef, item.BundleVersion = row.BundleRef, row.BundleVersion
		out = append(out, item)
	}
	return out, nil
}

func (r *RunRepository) UpdateStatus(ctx context.Context, runID string, status run.Status, errMsg string) error {
	params := store.UpdateBundleRunStatusParams{
		ID: runID, Status: string(status), FinishedAt: pgtype.Timestamptz{Valid: true, Time: time.Now()},
	}
	if errMsg != "" {
		params.Error = pgtype.Text{String: errMsg, Valid: true}
	}
	return r.q.UpdateBundleRunStatus(ctx, params)
}

func (r *RunRepository) MarkCancelRequested(ctx context.Context, runID string) error {
	return r.q.MarkBundleRunCancelRequested(ctx, runID)
}

func (r *RunRepository) AddUsage(ctx context.Context, runID string, tokens int64, costUSD float64) error {
	var cost pgtype.Numeric
	if err := cost.Scan(fmt.Sprintf("%.6f", costUSD)); err != nil {
		return fmt.Errorf("encode cost: %w", err)
	}
	return r.q.UpdateBundleRunUsage(ctx, store.UpdateBundleRunUsageParams{ID: runID, TotalTokens: tokens, CostUsd: cost})
}

func toDomainRun(row store.BundleRun) run.Run {
	out := run.Run{
		ID: row.ID, BundleID: row.BundleID, TriggeredBy: row.TriggeredBy,
		Status: run.Status(row.Status), CreatedAt: row.CreatedAt.Time,
		Usage: run.Usage{TotalTokens: row.TotalTokens, CostUSD: numericFloat(row.CostUsd)},
	}
	if row.ViaListingID.Valid {
		id := row.ViaListingID.Int64
		out.ViaListingID = &id
	}
	if row.Error.Valid {
		out.Error = row.Error.String
	}
	if row.FinishedAt.Valid {
		t := row.FinishedAt.Time
		out.FinishedAt = &t
	}
	if len(row.SharedState) > 0 {
		_ = json.Unmarshal(row.SharedState, &out.SharedState)
	}
	return out
}

func numericFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// ── Events ───────────────────────────────────────────────────────────

// RunEventStore implements run.EventStore.
type RunEventStore struct{ q store.Querier }

func NewRunEventStore(q store.Querier) *RunEventStore { return &RunEventStore{q: q} }

func (s *RunEventStore) Append(ctx context.Context, ev run.Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	_, err = s.q.InsertBundleRunEvent(ctx, store.InsertBundleRunEventParams{
		RunID: ev.RunID, Type: ev.Type, Node: pgtype.Text{String: ev.Node, Valid: ev.Node != ""},
		Payload: payload, IsInternal: ev.IsInternal,
	})
	return err
}

// ListAfter uses two separate queries rather than filtering in Go: the
// black-box subset is decided by SQL, so an internal event never leaves
// the database on a subscriber's behalf.
func (s *RunEventStore) ListAfter(ctx context.Context, runID string, afterID int64, includeInternal bool) ([]run.Event, error) {
	var rows []store.BundleRunEvent
	var err error
	if includeInternal {
		rows, err = s.q.ListBundleRunEventsAfter(ctx, store.ListBundleRunEventsAfterParams{RunID: runID, ID: afterID})
	} else {
		rows, err = s.q.ListBundleRunEventsAfterExternal(ctx, store.ListBundleRunEventsAfterExternalParams{RunID: runID, ID: afterID})
	}
	if err != nil {
		return nil, err
	}
	out := make([]run.Event, 0, len(rows))
	for _, row := range rows {
		ev := run.Event{ID: row.ID, RunID: row.RunID, Type: row.Type, IsInternal: row.IsInternal, CreatedAt: row.CreatedAt.Time}
		if row.Node.Valid {
			ev.Node = row.Node.String
		}
		_ = json.Unmarshal(row.Payload, &ev.Payload)
		out = append(out, ev)
	}
	return out, nil
}

// ── Human gates ──────────────────────────────────────────────────────

// GateRepository implements run.GateRepository.
type GateRepository struct{ q store.Querier }

func NewGateRepository(q store.Querier) *GateRepository { return &GateRepository{q: q} }

func (r *GateRepository) CreatePending(ctx context.Context, runID string, cfg run.GateConfig) (run.Gate, error) {
	var timeoutSeconds pgtype.Int4
	if cfg.TimeoutSeconds != nil {
		timeoutSeconds = pgtype.Int4{Valid: true, Int32: *cfg.TimeoutSeconds}
	}
	roles := cfg.ApproverRoles
	if roles == nil {
		roles = []string{}
	}
	approverRoles, err := json.Marshal(roles)
	if err != nil {
		return run.Gate{}, err
	}
	row, err := r.q.CreateHumanGate(ctx, store.CreateHumanGateParams{
		RunID: runID, Node: cfg.Node, TimeoutSeconds: timeoutSeconds,
		OnTimeout: string(cfg.OnTimeout), ApproverRoles: approverRoles,
	})
	if err != nil {
		return run.Gate{}, err
	}
	return toDomainGate(row), nil
}

func (r *GateRepository) FindPending(ctx context.Context, runID, node string) (run.Gate, error) {
	row, err := r.q.GetPendingHumanGateForRunNode(ctx, store.GetPendingHumanGateForRunNodeParams{RunID: runID, Node: node})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Gate{}, run.ErrNotFound
	}
	if err != nil {
		return run.Gate{}, err
	}
	return toDomainGate(row), nil
}

// Resolve updates only a still-pending row, so a decision that lost a race
// comes back as ErrGateResolved instead of overwriting the winner.
func (r *GateRepository) Resolve(ctx context.Context, gateID int64, d run.Decision, resolvedBy *int64) error {
	var by pgtype.Int8
	if resolvedBy != nil {
		by = pgtype.Int8{Valid: true, Int64: *resolvedBy}
	}
	var comment pgtype.Text
	if d.Reason != "" {
		comment = pgtype.Text{String: d.Reason, Valid: true}
	}
	_, err := r.q.ResolveHumanGate(ctx, store.ResolveHumanGateParams{
		ID: gateID, Status: string(d.Status), ResolvedBy: by, Comment: comment,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrGateResolved
	}
	return err
}

func (r *GateRepository) ListPastTimeout(ctx context.Context) ([]run.Gate, error) {
	rows, err := r.q.ListPendingHumanGatesPastTimeout(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]run.Gate, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainGate(row))
	}
	return out, nil
}

func toDomainGate(row store.HumanGate) run.Gate {
	return run.Gate{
		ID: row.ID, RunID: row.RunID, Node: row.Node,
		Status: run.GateStatus(row.Status), OnTimeout: run.ParseTimeoutPolicy(row.OnTimeout),
		CreatedAt: row.CreatedAt.Time,
	}
}

// ── Audit log ────────────────────────────────────────────────────────

// AuditLogWriter implements run.AuditLog.
type AuditLogWriter struct{ q store.Querier }

func NewAuditLogWriter(q store.Querier) *AuditLogWriter { return &AuditLogWriter{q: q} }

func (w *AuditLogWriter) Record(ctx context.Context, actorUserID *int64, action, targetType, targetID string, detail map[string]any) error {
	body, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	var actor pgtype.Int8
	if actorUserID != nil {
		actor = pgtype.Int8{Valid: true, Int64: *actorUserID}
	}
	_, err = w.q.CreateAuditLog(ctx, store.CreateAuditLogParams{
		ActorUserID: actor, Action: action, TargetType: targetType, TargetID: targetID, Detail: body,
	})
	return err
}
