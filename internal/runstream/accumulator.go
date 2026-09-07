package runstream

import (
	"sync"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// maxAccumulatedChars 是单个节点累积文本的上限。
//
// 缓冲活在内存里，而且一次运行可能有多个节点、一台机器可能同时跑很多次运
// 行，所以必须有个封顶——一个跑飞了的模型不该把进程撑爆。超过之后丢弃最早
// 的部分（保留末尾），因为中途接入的人最需要看到的是"现在写到哪了"。
const maxAccumulatedChars = 256 << 10

// accumulator 攒着每次运行里每个节点"到目前为止"的正文与思维链。
//
// 这是"历史存消息、传输走 token"这个分工里，唯一需要额外设计的一块：逐
// token 的增量不落库之后，中途刷新页面的人就没有地方拿到已经生成的那半段
// 文字了。缓冲把它补上——重连时合成一条 node.snapshot 下发。
//
// 它是**进程内**的，和 Broker 一样：进程重启、或者多副本部署时连到另一个
// 副本，快照就没有了，那种情况下页面会停在已落库的消息上、等这一轮的
// node.finished。这和业界拿 Redis 存可恢复流的缓冲是同一组取舍，换成
// Redis 时替换这个类型即可，上面的接口不用动。
type accumulator struct {
	mu   sync.Mutex
	runs map[string]map[string]*nodeText // runID -> node -> 累积
}

type nodeText struct {
	text      []rune
	reasoning []rune
}

func newAccumulator() *accumulator {
	return &accumulator{runs: make(map[string]map[string]*nodeText)}
}

// add 累加一条增量。ephemeral 事件的 payload 只有 {"text": "..."}。
func (a *accumulator) add(ev run.Event) {
	text, _ := ev.Payload["text"].(string)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	nodes := a.runs[ev.RunID]
	if nodes == nil {
		nodes = make(map[string]*nodeText)
		a.runs[ev.RunID] = nodes
	}
	nt := nodes[ev.Node]
	if nt == nil {
		nt = &nodeText{}
		nodes[ev.Node] = nt
	}
	// 按 rune 而不是 byte 截断：中文一个字三个字节，按字节切会把一个字劈
	// 成半个，快照里就会出现乱码。
	switch ev.Type {
	case run.EventNodeReasoning:
		nt.reasoning = capRunes(append(nt.reasoning, []rune(text)...))
	default:
		nt.text = capRunes(append(nt.text, []rune(text)...))
	}
}

func capRunes(r []rune) []rune {
	if len(r) <= maxAccumulatedChars {
		return r
	}
	return r[len(r)-maxAccumulatedChars:]
}

// snapshot 返回这次运行每个节点"到目前为止"的文字，一个节点一条事件。
// 中途接入的客户端靠它把界面恢复到当前状态。
func (a *accumulator) snapshot(runID string) []run.Event {
	a.mu.Lock()
	defer a.mu.Unlock()

	nodes := a.runs[runID]
	out := make([]run.Event, 0, len(nodes))
	for node, nt := range nodes {
		if len(nt.text) == 0 && len(nt.reasoning) == 0 {
			continue
		}
		out = append(out, run.Event{
			RunID: runID, Type: run.EventNodeSnapshot, Node: node, Ephemeral: true,
			Payload: map[string]any{"text": string(nt.text), "reasoning": string(nt.reasoning)},
		})
	}
	return out
}

// takeNode 取走并清空某个节点的累积，用于它跑完时把该留的落成一条。
func (a *accumulator) takeNode(runID, node string) (text, reasoning string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	nodes := a.runs[runID]
	nt := nodes[node]
	if nt == nil {
		return "", ""
	}
	delete(nodes, node)
	return string(nt.text), string(nt.reasoning)
}

// takeRun 取走并清空整次运行的累积，用于运行到达终态时收尾。返回的 map
// 按节点名索引，顺序由调用方决定。
func (a *accumulator) takeRun(runID string) map[string]*nodeText {
	a.mu.Lock()
	defer a.mu.Unlock()
	nodes := a.runs[runID]
	delete(a.runs, runID)
	return nodes
}
