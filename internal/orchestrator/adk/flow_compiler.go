package adk

import (
	"fmt"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/session"
)

// FlowCompileOptions carries what CompileFlow needs to turn a RunTypeFlow
// Bundle into an executable ADK agent: agents[] run exactly once, in
// declaration order, with no conditions, branching, parallelism or
// self-loop retries. That ordering is exactly what ADK's own
// SequentialAgent already provides — unlike CompileBundle, this package
// owns no scheduling logic of its own for a flow; the graph-walking
// executor in bundle_compiler.go is never involved.
type FlowCompileOptions struct {
	BundleRef string
	// Nodes is agents[] in declaration order — the exact order sub-agents
	// run in. There is no orchestration.edges block to derive this from;
	// the DSL's own array order is the schedule.
	Nodes  []string
	Agents map[string]agent.Agent
	// GateNodes are the node names with a human_gates[].after entry.
	GateNodes  map[string]bool
	GateWaiter GateWaiter // nil: gates are structurally present but skipped
}

// CompileFlow compiles opts into a root ADK agent built from ADK's native
// SequentialAgent — a genuinely different dispatch path from
// CompileBundle's hand-rolled graph walk, not the same engine restricted to
// a linear shape.
func CompileFlow(opts FlowCompileOptions) (agent.Agent, error) {
	if len(opts.Nodes) == 0 {
		return nil, fmt.Errorf("adk: bundle %q: flow has no agents", opts.BundleRef)
	}

	subAgents := make([]agent.Agent, 0, len(opts.Nodes))
	for _, node := range opts.Nodes {
		a, ok := opts.Agents[node]
		if !ok {
			return nil, fmt.Errorf("adk: bundle %q: no compiled agent for node %q", opts.BundleRef, node)
		}
		if opts.GateNodes[node] && opts.GateWaiter != nil {
			wrapped, err := gateWrap(node, a, opts.GateWaiter)
			if err != nil {
				return nil, fmt.Errorf("adk: bundle %q: wrap gate for node %q: %w", opts.BundleRef, node, err)
			}
			a = wrapped
		}
		subAgents = append(subAgents, a)
	}

	root, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: opts.BundleRef, SubAgents: subAgents},
	})
	if err != nil {
		return nil, fmt.Errorf("adk: bundle %q: compile flow: %w", opts.BundleRef, err)
	}
	return root, nil
}

// gateWrap runs a node's real agent to completion, then blocks on its
// human_gates[].after wait before control passes to the next step in the
// sequence. A flow has no executor of its own to hook a gate into (unlike
// CompileBundle's, which waits between scheduling steps) — SequentialAgent
// just runs each sub-agent's Run in turn — so the wait has to be woven into
// the sub-agent itself.
func gateWrap(node string, inner agent.Agent, waiter GateWaiter) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name: node,
		Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				for ev, err := range inner.Run(ic) {
					if !yield(ev, err) {
						return
					}
					if err != nil {
						return
					}
				}
				if err := waiter.Wait(ic, node); err != nil {
					yield(nil, fmt.Errorf("adk: human gate after %q: %w", node, err))
				}
			}
		},
	})
}

// CompileSingle returns node's already-compiled agent unchanged: a
// RunTypeSingle Bundle has exactly one agent and no orchestration layer at
// all, so there is nothing for this package to build — the sole point of
// this function is to give that "no compilation step" a name symmetric with
// CompileBundle/CompileFlow, and to fail clearly if the node is missing.
func CompileSingle(bundleRef, node string, agents map[string]agent.Agent) (agent.Agent, error) {
	a, ok := agents[node]
	if !ok {
		return nil, fmt.Errorf("adk: bundle %q: no compiled agent for node %q", bundleRef, node)
	}
	return a, nil
}
