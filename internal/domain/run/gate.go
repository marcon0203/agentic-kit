package run

import "time"

// GateStatus is a human gate's resolution state.
type GateStatus string

const (
	GateStatusPending  GateStatus = "pending"
	GateStatusApproved GateStatus = "approved"
	GateStatusRejected GateStatus = "rejected"
	// GateStatusTimeout is distinct from rejected on purpose: "nobody
	// answered" and "somebody said no" are different facts in the audit
	// log, even though both stop the branch.
	GateStatusTimeout GateStatus = "timeout"
)

// TimeoutPolicy is Bundle.orchestration.human_gates[].on_timeout — what to
// do when the deadline passes with no human decision.
type TimeoutPolicy string

const (
	TimeoutAbort       TimeoutPolicy = "abort"
	TimeoutAutoApprove TimeoutPolicy = "auto_approve"
	TimeoutAutoReject  TimeoutPolicy = "auto_reject"
)

// ParseTimeoutPolicy defaults to abort, which is the safe reading of an
// unrecognised or absent policy: a gate exists because someone wanted a
// decision, so silence must not become approval.
func ParseTimeoutPolicy(s string) TimeoutPolicy {
	switch TimeoutPolicy(s) {
	case TimeoutAutoApprove:
		return TimeoutAutoApprove
	case TimeoutAutoReject:
		return TimeoutAutoReject
	default:
		return TimeoutAbort
	}
}

// Apply maps a policy to the outcome the scanner should record when the
// deadline passes.
func (p TimeoutPolicy) Apply() Decision {
	switch p {
	case TimeoutAutoApprove:
		return Decision{Status: GateStatusApproved, Approved: true}
	case TimeoutAutoReject:
		return Decision{Status: GateStatusRejected}
	default:
		return Decision{Status: GateStatusTimeout}
	}
}

// Decision is how a gate ended. Reason becomes the blocked branch's error
// when the gate was not approved.
type Decision struct {
	Status   GateStatus
	Approved bool
	Reason   string
}

// GateConfig is one human_gates[] entry, parsed once at compile time.
type GateConfig struct {
	Node           string
	TimeoutSeconds *int32
	OnTimeout      TimeoutPolicy
	ApproverRoles  []string
}

// Gate is a persisted pending approval.
type Gate struct {
	ID        int64
	RunID     string
	Node      string
	Status    GateStatus
	OnTimeout TimeoutPolicy
	CreatedAt time.Time
}

// ParseGateConfigs reads the human gates out of a Bundle definition. An
// entry with no `after` node names nothing to gate and is skipped.
func ParseGateConfigs(bundleDef map[string]any) map[string]GateConfig {
	orch, _ := bundleDef["orchestration"].(map[string]any)
	raw, _ := orch["human_gates"].([]any)

	configs := make(map[string]GateConfig, len(raw))
	for _, g := range raw {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		after, _ := gm["after"].(string)
		if after == "" {
			continue
		}
		policy, _ := gm["on_timeout"].(string)
		cfg := GateConfig{Node: after, OnTimeout: ParseTimeoutPolicy(policy), ApproverRoles: stringSlice(gm["approver_roles"])}
		if ts, ok := gm["timeout_seconds"].(float64); ok {
			v := int32(ts)
			cfg.TimeoutSeconds = &v
		}
		configs[after] = cfg
	}
	return configs
}

// ParseLimits reads Bundle.limits. Absent fields stay zero, meaning
// unbounded.
func ParseLimits(bundleDef map[string]any) Limits {
	l, _ := bundleDef["limits"].(map[string]any)
	var out Limits
	if v, ok := l["max_total_tokens"].(float64); ok {
		out.MaxTotalTokens = int64(v)
	}
	if v, ok := l["max_cost_usd"].(float64); ok {
		out.MaxCostUSD = v
	}
	if v, ok := l["max_wall_clock_seconds"].(float64); ok {
		out.MaxWallClockSeconds = int64(v)
	}
	return out
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
