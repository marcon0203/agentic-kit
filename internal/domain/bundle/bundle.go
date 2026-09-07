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

// SystemAgentTestRef is the one Bundle ref the platform creates on a user's
// behalf rather than the user authoring it: the placeholder that 草稿试运行
// runs hang off, because bundle_runs.bundle_id is a NOT NULL foreign key
// (see run.AgentTestBundleProvider). It is filtered out of the Bundle list —
// it is plumbing, not one of the user's applications. The leading
// underscores are also outside the ref pattern the API accepts, so a user
// can never author a Bundle that collides with it.
const SystemAgentTestRef = "__agent_test__"

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

// RunType is how a Bundle's agents[] get scheduled at run time — a real
// difference in orchestrator dispatch, not a DSL-authoring convenience
// (spec: "不同的类型的 bundle 运行模式不一样的").
type RunType string

const (
	// RunTypeGraph is the general case: orchestration.edges are walked by
	// the graph engine — conditions, parallel fan-out, join, self-loop
	// retries, human gates. Everything else is a restricted special case
	// of this.
	RunTypeGraph RunType = "graph"
	// RunTypeFlow runs agents[] once, strictly in declaration order, with
	// no conditions/branching/parallelism — compiled onto ADK's own
	// SequentialAgent instead of the graph engine, so there is no
	// orchestration block to author at all.
	RunTypeFlow RunType = "flow"
	// RunTypeSingle is exactly one agent, run directly with no
	// orchestration layer whatsoever.
	RunTypeSingle RunType = "single"
	// RunTypeRouter 让**模型**决定下一步交给谁，而不是让 DSL 决定。
	//
	// 和 RunTypeGraph 的区别是"谁在做路由决策"：graph 的边上挂的是布尔表
	// 达式（`shared_state.score > 0.8`），走哪条边编译期就定死了；router
	// 把 agents[] 里的一个 Agent 指定为路由器，其余成为它的候选，由它每
	// 一步看着上下文决定交给谁、或者收尾。这是 supervisor / handoff 那一
	// 类形态，用条件边表达不出来——条件边不认识"意图"。
	//
	// 底座是 ADK 原生的 transfer 机制：LlmAgent 一旦带上 SubAgents，
	// AgentTransferRequestProcessor 会自动给它挂上 transfer_to_agent 工具
	// 和相应指令，base_flow 负责把控制权切过去。所以这里不需要自己写调度
	// 循环，只需要把编译好的候选接成父子关系。
	RunTypeRouter RunType = "router"
)

// Type reads the DSL's optional top-level `type`, defaulting to
// RunTypeGraph — every Bundle saved before this field existed is a graph
// Bundle, so an absent field must mean exactly that, not an error.
func (d Definition) Type() RunType {
	if s, _ := d["type"].(string); s != "" {
		return RunType(s)
	}
	return RunTypeGraph
}

// RouterConfig 是 RunTypeRouter 的编排配置，读自 DSL 顶层的 `router` 块。
type RouterConfig struct {
	// Node 是充当路由器的那个节点名（agents[] 里某项的 alias，没有 alias
	// 就是 ref）。其余节点都是它的候选。
	Node string
	// MaxHandoffs 是一次运行里控制权最多转交几次，0 表示用默认值。它兜住
	// 的是"两个 Agent 互相踢皮球"——模型自己不会喊停，而 ADK 的 transfer
	// 没有内建次数上限（只有 LoopAgent 有 MaxIterations）。语义和 graph
	// 的 maxLoop 对齐：超限即失败，而不是悄悄截断。
	MaxHandoffs int
}

// Router reads the DSL's top-level `router` block. 只有 RunTypeRouter 用得上。
func (d Definition) Router() RouterConfig {
	m, _ := d["router"].(map[string]any)
	cfg := RouterConfig{}
	if m == nil {
		return cfg
	}
	cfg.Node, _ = m["node"].(string)
	// JSON 数字过 encoding/json 之后是 float64；DSL 也可能来自 Go 侧构造，
	// 那时是 int。两种都认。
	switch n := m["max_handoffs"].(type) {
	case float64:
		cfg.MaxHandoffs = int(n)
	case int:
		cfg.MaxHandoffs = n
	}
	return cfg
}

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
