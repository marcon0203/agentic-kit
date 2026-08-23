package adk

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

func TestADKRunner_Run_EmitsTranslatedEventsAndFinalText(t *testing.T) {
	root := mockLLMAgent(t, "single_agent", func(ic agent.InvocationContext) *session.Event {
		return finalTextEvent(ic, "the answer", nil)
	})

	r, err := NewADKRunner("test-app", root)
	if err != nil {
		t.Fatalf("NewADKRunner: %v", err)
	}

	var events []Event
	out, err := r.Run(context.Background(), AgentInput{UserID: "u1", SessionID: "s1", Message: "go"}, func(ev Event) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.FinalText != "the answer" {
		t.Fatalf("expected FinalText %q, got %q", "the answer", out.FinalText)
	}

	found := false
	for _, ev := range events {
		if ev.Type == EventNodeFinished && ev.Node == "single_agent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a node.finished event for single_agent, got %+v", events)
	}
}

func TestADKRunner_Run_PropagatesAgentError(t *testing.T) {
	failing, err := agent.New(agent.Config{
		Name: "failing_agent",
		Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(nil, context.DeadlineExceeded)
			}
		},
	})
	if err != nil {
		t.Fatalf("build failing agent: %v", err)
	}

	r, err := NewADKRunner("test-app", failing)
	if err != nil {
		t.Fatalf("NewADKRunner: %v", err)
	}
	_, err = r.Run(context.Background(), AgentInput{UserID: "u1", SessionID: "s1", Message: "go"}, nil)
	if err == nil {
		t.Fatal("expected the agent's error to propagate")
	}
}
