package adk

import (
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

func TestCompileFlow_RunsAgentsInDeclarationOrder(t *testing.T) {
	var order []string

	agents := map[string]agent.Agent{
		"a": mockLLMAgent(t, "a", func(ic agent.InvocationContext) *session.Event {
			order = append(order, "a")
			return finalTextEvent(ic, "a done", nil)
		}),
		"b": mockLLMAgent(t, "b", func(ic agent.InvocationContext) *session.Event {
			order = append(order, "b")
			return finalTextEvent(ic, "b done", nil)
		}),
		"c": mockLLMAgent(t, "c", func(ic agent.InvocationContext) *session.Event {
			order = append(order, "c")
			return finalTextEvent(ic, "c done", nil)
		}),
	}

	root, err := CompileFlow(FlowCompileOptions{BundleRef: "seq", Nodes: []string{"a", "b", "c"}, Agents: agents})
	if err != nil {
		t.Fatalf("CompileFlow: %v", err)
	}
	runCompiled(t, root)

	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("expected strict declaration order [a b c], got %v", order)
	}
}

func TestCompileFlow_HumanGate_BlocksBeforeNextStep(t *testing.T) {
	var order []string

	agents := map[string]agent.Agent{
		"a": mockLLMAgent(t, "a", func(ic agent.InvocationContext) *session.Event {
			order = append(order, "a")
			return finalTextEvent(ic, "a done", nil)
		}),
		"b": mockLLMAgent(t, "b", func(ic agent.InvocationContext) *session.Event {
			order = append(order, "b")
			return finalTextEvent(ic, "b done", nil)
		}),
	}
	waiter := &mockGateWaiter{}

	root, err := CompileFlow(FlowCompileOptions{
		BundleRef: "seq", Nodes: []string{"a", "b"}, Agents: agents,
		GateNodes: map[string]bool{"a": true}, GateWaiter: waiter,
	})
	if err != nil {
		t.Fatalf("CompileFlow: %v", err)
	}
	runCompiled(t, root)

	if len(waiter.waited) != 1 || waiter.waited[0] != "a" {
		t.Fatalf("expected the gate after %q to be waited on exactly once, got %v", "a", waiter.waited)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("expected b to still run after the gate resolves, got order %v", order)
	}
}

func TestCompileFlow_UnknownNode_ReturnsCompileError(t *testing.T) {
	_, err := CompileFlow(FlowCompileOptions{BundleRef: "seq", Nodes: []string{"missing"}, Agents: map[string]agent.Agent{}})
	if err == nil {
		t.Fatal("expected an error when a flow node has no compiled agent")
	}
}

func TestCompileFlow_NoNodes_ReturnsCompileError(t *testing.T) {
	_, err := CompileFlow(FlowCompileOptions{BundleRef: "seq", Agents: map[string]agent.Agent{}})
	if err == nil {
		t.Fatal("expected an error when a flow has no agents at all")
	}
}

func TestCompileSingle_ReturnsTheOneAgentUnchanged(t *testing.T) {
	a := mockLLMAgent(t, "solo", func(ic agent.InvocationContext) *session.Event {
		return finalTextEvent(ic, "done", nil)
	})
	agents := map[string]agent.Agent{"solo": a}

	root, err := CompileSingle("one-off", "solo", agents)
	if err != nil {
		t.Fatalf("CompileSingle: %v", err)
	}
	if root != a {
		t.Fatalf("expected CompileSingle to return the same compiled agent unwrapped")
	}
}

func TestCompileSingle_UnknownNode_ReturnsCompileError(t *testing.T) {
	_, err := CompileSingle("one-off", "missing", map[string]agent.Agent{})
	if err == nil {
		t.Fatal("expected an error when the single node has no compiled agent")
	}
}
