package adk

import (
	"fmt"
	"iter"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// defaultMaxHandoffs 兜住"两个 Agent 互相踢皮球"。
//
// 模型自己不会喊停，而 ADK 的 transfer 机制没有内建次数上限（只有
// LoopAgent 有 MaxIterations），所以这一层必须由我们加。取 8：一次有意义
// 的多步协作很少超过这个数，而真的绕进死循环时，8 次之内就能停下来，不用
// 等运行的墙钟上限把整次运行拖到超时。
const defaultMaxHandoffs = 8

// RouterCompileOptions carries what CompileRouter needs to turn a
// RunTypeRouter Bundle into one root agent.
type RouterCompileOptions struct {
	BundleRef string
	// Router 是已经编译好的路由 Agent。它必须是**带着候选一起编译**出来的
	// （AgentCompileOptions.SubAgents），不能事后再挂：ADK 在
	// llmagent.New 时就把父子关系固化进 agent 树了，
	// AgentTransferRequestProcessor 靠它决定要不要给这个 Agent 挂上
	// transfer_to_agent 工具。
	Router agent.Agent
	// RouterNode 是路由器的节点名，只用于错误信息。
	RouterNode string
	// Candidates 是路由器可以转交的那些节点名，按 agents[] 声明顺序。
	Candidates []string
	// MaxHandoffs 覆盖 defaultMaxHandoffs；0 用默认值。
	MaxHandoffs int
}

// CompileRouter 把一个 RunTypeRouter Bundle 编译成根 agent。
//
// 和另外三种编译器的关系：
//
//   - CompileBundle 自己写了一套图遍历执行器，因为"按条件边调度"没有现成
//     的东西可用。
//   - CompileFlow 用 ADK 的 SequentialAgent。
//   - CompileRouter 什么调度循环都不用写：ADK 的 LlmAgent 一旦带上
//     SubAgents，AgentTransferRequestProcessor 就会自动给它挂上
//     transfer_to_agent 工具和相应指令，base_flow 负责把控制权切到目标
//     Agent。父子关系在编译 Router 时就已经建立（见 RouterCompileOptions
//     .Router 的注释），所以这里剩下的唯一职责是**给转交次数封顶**。
//
// 封顶为什么不能放在候选自己身上：候选是普通 llmagent，它不知道自己被转交
// 了第几次。计数必须在一个能看见每一次转交的地方，也就是包在候选外面的这
// 层壳里。
func CompileRouter(opts RouterCompileOptions) (agent.Agent, error) {
	if opts.Router == nil {
		return nil, fmt.Errorf("adk: bundle %q: router node %q was not compiled", opts.BundleRef, opts.RouterNode)
	}
	if len(opts.Candidates) == 0 {
		return nil, fmt.Errorf("adk: bundle %q: router %q has no candidate agents to hand off to",
			opts.BundleRef, opts.RouterNode)
	}
	return opts.Router, nil
}

// handoffCounter 数一次运行里控制权被转交了多少次。
//
// 一次运行编译一棵新的 agent 树（engine.Prepare 是按 runID 调的），所以一
// 个计数器天然就是"这次运行"的作用域，不需要按 invocation id 分桶。加锁是
// 因为并行分支理论上可能同时进来——ADK 的 transfer 本身是串行的，但候选
// Agent 内部可以有并行工具调用。
type HandoffCounter struct {
	mu    sync.Mutex
	count int
	max   int
}

// NewHandoffCounter 建一次运行的转交计数器。引擎持有它，因为包壳这件事
// 发生在引擎那侧（候选是在那里编译出来的）。
func NewHandoffCounter(max int) *HandoffCounter {
	if max <= 0 {
		max = defaultMaxHandoffs
	}
	return &HandoffCounter{max: max}
}

// take 记一次转交。返回 false 表示已经超限。
func (c *HandoffCounter) take() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	return c.count <= c.max
}

// CountHandoffs 把候选包一层，每次它拿到控制权就记一次数，超限就让这次运行
// 失败。
//
// 失败而不是悄悄停下，是刻意的，和 CompileBundle 对自循环超限的处理一致：
// 悄悄截断会产出一个"看起来正常结束、其实半途而废"的结果，用户没有任何线索
// 知道发生了什么；明确失败至少能重试、能改编排。
// CountHandoffs 把候选包一层，见下方注释。
func CountHandoffs(node string, inner agent.Agent, c *HandoffCounter) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name: node,
		Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				if !c.take() {
					yield(nil, fmt.Errorf(
						"adk: 转交次数超过上限 (%d)：路由器和候选之间可能在互相推诿，"+
							"检查各 Agent 的人设是否都把工作推给别人，或调大 router.max_handoffs", c.max))
					return
				}
				for ev, err := range inner.Run(ic) {
					if !yield(ev, err) {
						return
					}
					if err != nil {
						return
					}
				}
			}
		},
	})
}
