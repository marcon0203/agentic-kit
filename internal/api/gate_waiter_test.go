package api

import (
	"context"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/store"
)

type fakeGateWaiterQuerier struct {
	store.Querier

	events    []store.BundleRunEvent
	nextID    int64
	createErr error
}

func (f *fakeGateWaiterQuerier) CreateHumanGate(_ context.Context, arg store.CreateHumanGateParams) (store.HumanGate, error) {
	if f.createErr != nil {
		return store.HumanGate{}, f.createErr
	}
	f.nextID++
	return store.HumanGate{ID: f.nextID, RunID: arg.RunID, Node: arg.Node, Status: "pending", OnTimeout: arg.OnTimeout}, nil
}

func (f *fakeGateWaiterQuerier) InsertBundleRunEvent(_ context.Context, arg store.InsertBundleRunEventParams) (store.BundleRunEvent, error) {
	ev := store.BundleRunEvent{ID: int64(len(f.events) + 1), RunID: arg.RunID, Type: arg.Type, Node: arg.Node, Payload: arg.Payload, IsInternal: arg.IsInternal}
	f.events = append(f.events, ev)
	return ev, nil
}

// spec-14's ds-gate-card is driven entirely by the human_gate.waiting
// event landing in the run's event stream — this asserts Wait() persists
// it (as an external event, node-tagged) before it blocks.
func TestRunGateWaiter_Wait_EmitsHumanGateWaitingEvent(t *testing.T) {
	f := &fakeGateWaiterQuerier{}
	registry := newGateRegistry()
	waiter := newRunGateWaiter(f, registry, "run-1", map[string]gateNodeConfig{
		"architect": {OnTimeout: "abort"},
	})

	done := make(chan error, 1)
	go func() { done <- waiter.Wait(context.Background(), "architect") }()

	// Give Wait() time to persist the event and register before resolving.
	time.Sleep(20 * time.Millisecond)
	if len(f.events) != 1 {
		t.Fatalf("expected 1 human_gate.waiting event, got %+v", f.events)
	}
	if f.events[0].Type != "human_gate.waiting" || f.events[0].RunID != "run-1" || !f.events[0].Node.Valid || f.events[0].Node.String != "architect" {
		t.Fatalf("unexpected event: %+v", f.events[0])
	}
	if f.events[0].IsInternal {
		t.Fatal("human_gate.waiting must be external — subscribers need to see the gate card")
	}

	registry.resolve(f.nextID, gateResolution{approved: true})
	if err := <-done; err != nil {
		t.Fatalf("expected Wait to succeed once approved, got %v", err)
	}
}
