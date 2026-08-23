// Package bundle is the Bundle bounded context: an orchestration definition
// over several Agents, and the static-validation rules that decide whether
// one may be saved.
//
// spec-07's central distinction lives here: some graph problems are
// *blocking* (a node that can never fire makes the Bundle unrunnable) while
// others are *warnings* returned alongside a successful save (an unreachable
// node, or a handoff declaration that has drifted from the actual edges).
// The second kind must never block, because the Agent DSL and the Bundle DSL
// can be maintained by different people.
package bundle

import "time"

// Status is a Bundle version's lifecycle flag.
type Status int16

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

// Bundle is one immutable version of an orchestration definition.
type Bundle struct {
	ID         int64
	OwnerID    int64
	Ref        string
	Version    string
	Definition Definition
	Status     Status
	CreatedAt  time.Time
}

// Definition is the Bundle DSL document.
type Definition map[string]any

func (d Definition) Ref() string     { s, _ := d["bundle"].(string); return s }
func (d Definition) Version() string { s, _ := d["version"].(string); return s }

// AgentBinding is one entry of the Bundle's agents[]. Node is the name the
// orchestration graph refers to: the alias when there is one, otherwise the
// Agent's own ref — which is what lets the same Agent appear twice in one
// Bundle under different node names.
type AgentBinding struct {
	Node string
	Ref  string
}

// Agents reads the agents[] block into node -> agent-ref bindings.
func (d Definition) Agents() []AgentBinding {
	raw, _ := d["agents"].([]any)
	out := make([]AgentBinding, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := m["ref"].(string)
		node, _ := m["alias"].(string)
		if node == "" {
			node = ref
		}
		out = append(out, AgentBinding{Node: node, Ref: ref})
	}
	return out
}

// Handoff is an Agent's own declaration of who it expects to exchange work
// with. It is advisory: the Bundle's edges are the source of truth for what
// actually runs.
type Handoff struct {
	AcceptsInputFrom []string
	ProducesOutputTo []string
}

func (h Handoff) accepts(ref string) bool    { return contains(h.AcceptsInputFrom, ref) }
func (h Handoff) producesTo(ref string) bool { return contains(h.ProducesOutputTo, ref) }

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Warning is a non-blocking problem reported alongside a saved Bundle.
type Warning struct {
	Field  string
	Reason string
}

// CreateResult is a saved Bundle plus whatever the graph and handoff checks
// wanted to say about it without refusing the save.
type CreateResult struct {
	Bundle   Bundle
	Warnings []Warning
}
