package api

import (
	"context"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/store"
)

type fakeGateTimeoutQuerier struct {
	store.Querier

	pending []store.HumanGate
	gates   map[int64]store.HumanGate
	audits  []store.AuditLog
}

func (f *fakeGateTimeoutQuerier) ListPendingHumanGatesPastTimeout(_ context.Context) ([]store.HumanGate, error) {
	return f.pending, nil
}

func (f *fakeGateTimeoutQuerier) ResolveHumanGate(_ context.Context, arg store.ResolveHumanGateParams) (store.HumanGate, error) {
	g := f.gates[arg.ID]
	g.Status = arg.Status
	f.gates[arg.ID] = g
	return g, nil
}

func (f *fakeGateTimeoutQuerier) CreateAuditLog(_ context.Context, arg store.CreateAuditLogParams) (store.AuditLog, error) {
	row := store.AuditLog{ID: int64(len(f.audits) + 1), Action: arg.Action, TargetType: arg.TargetType, TargetID: arg.TargetID}
	f.audits = append(f.audits, row)
	return row, nil
}

func TestGateTimeoutScanner_AutoApprove(t *testing.T) {
	gate := store.HumanGate{ID: 1, RunID: "run-1", Node: "review", OnTimeout: "auto_approve"}
	f := &fakeGateTimeoutQuerier{pending: []store.HumanGate{gate}, gates: map[int64]store.HumanGate{1: gate}}
	registry := newGateRegistry()
	ch := registry.register(1)

	scanOnce(context.Background(), f, registry, nil)

	if f.gates[1].Status != "approved" {
		t.Fatalf("expected gate resolved to approved, got %+v", f.gates[1])
	}
	select {
	case res := <-ch:
		if !res.approved {
			t.Fatalf("expected approved resolution, got %+v", res)
		}
	default:
		t.Fatal("expected registry to receive a resolution")
	}
}

func TestGateTimeoutScanner_Abort(t *testing.T) {
	gate := store.HumanGate{ID: 2, RunID: "run-1", Node: "review", OnTimeout: "abort"}
	f := &fakeGateTimeoutQuerier{pending: []store.HumanGate{gate}, gates: map[int64]store.HumanGate{2: gate}}
	registry := newGateRegistry()
	ch := registry.register(2)

	scanOnce(context.Background(), f, registry, nil)

	if f.gates[2].Status != "timeout" {
		t.Fatalf("expected gate resolved to timeout, got %+v", f.gates[2])
	}
	select {
	case res := <-ch:
		if res.approved {
			t.Fatalf("expected rejected resolution, got %+v", res)
		}
	default:
		t.Fatal("expected registry to receive a resolution")
	}
	if len(f.audits) != 1 {
		t.Fatalf("expected one audit log row, got %+v", f.audits)
	}
}
