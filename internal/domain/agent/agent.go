// Package agent is the Agent bounded context: an Agent definition, the
// invariants that govern creating and deleting one, and the ports it needs
// from the outside world. It knows nothing about HTTP or SQL.
package agent

import "time"

// Status is an Agent version's lifecycle flag. The platform stores it as a
// smallint; the domain speaks in names.
type Status int16

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

func (s Status) Enabled() bool { return s == StatusEnabled }

// Agent is one *version* of an Agent definition. Versions are immutable
// once published (migration 0006 enforces it at the DB level too), so there
// is no Update operation in this context — a change is a new version.
type Agent struct {
	ID         int64
	OwnerID    int64
	Ref        string
	Version    string
	Definition Definition
	Status     Status
	CreatedAt  time.Time
}

// Definition is the Agent DSL document as a value object. Keeping the raw
// map behind accessors means the DSL's shape is interpreted in exactly one
// place instead of being re-dug with map lookups at each call site.
type Definition map[string]any

// Ref is definition.agent — the DSL, not the caller, names the Agent.
func (d Definition) Ref() string { return d.str("agent") }

// Version is definition.version.
func (d Definition) Version() string { return d.str("version") }

// Tools is capabilities.tools[] — refs that may name a Tool, an MCP server
// or a knowledge base (see the ResourceCatalog port).
func (d Definition) Tools() []string { return d.strSlice("capabilities", "tools") }

// Skills is capabilities.skills[].
func (d Definition) Skills() []string { return d.strSlice("capabilities", "skills") }

// ModelProvider is model.provider — 这个 Agent 主模型走哪个渠道。
func (d Definition) ModelProvider() string { return d.nestedStr("model", "provider") }

// ModelFallbacks is model.fallback[] —— "provider/模型名" 形式的降级链。
func (d Definition) ModelFallbacks() []string { return d.strSlice("model", "fallback") }

func (d Definition) nestedStr(path ...string) string {
	var cur any = map[string]any(d)
	for _, key := range path {
		asMap, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = asMap[key]
	}
	s, _ := cur.(string)
	return s
}

func (d Definition) str(key string) string {
	s, _ := d[key].(string)
	return s
}

func (d Definition) strSlice(path ...string) []string {
	var cur any = map[string]any(d)
	for _, key := range path {
		asMap, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = asMap[key]
	}
	arr, ok := cur.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// RefStatus is the result of resolving a capability reference against the
// owner's resource catalog: whether it exists at all, and whether it is
// currently enabled. Both matter — spec-06 requires 30002 for either, but
// the two produce different `details` wording.
type RefStatus struct {
	Found   bool
	Enabled bool
}

// BundleRef identifies a Bundle version that references an Agent. It is a
// deliberately narrow read model, not the Bundle entity: this context needs
// only enough to explain *why* a delete was refused.
type BundleRef struct {
	Ref     string
	Version string
}
