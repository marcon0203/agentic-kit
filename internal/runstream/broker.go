// Package runstream 是运行事件的**实时**那一半。
//
// 落库和推流原本是同一条线：事件写进 bundle_run_events，SSE 连接每 300ms
// 回头查一次库看有没有新行。那个形态的代价全压在数据库上——每条连接每秒
// 6.7 次查询（状态一次、事件一次），100 个人同时在跑就是 ~667 QPS 的空转
// 查询，其中绝大多数返回 0 行。模型吐得越快，轮询越跟不上，前端看到的还
// 是"每 300ms 蹦一块"，不是流。
//
// 现在两条线分开：
//
//   - 写：引擎照常 INSERT。这条不能省——刷新页面要重建对话、运行详情要回
//     放执行过程、断线要靠自增 id 续传，都得有持久化的事件。
//   - 读：INSERT 成功后把事件推给进程内的订阅者（PublishingStore），SSE
//     连接阻塞等这个 channel，稳态下一次库都不查。
//
// 广播器是**进程内**的，这是它的边界：多副本部署时，A 副本上跑的运行推不
// 到连在 B 副本上的浏览器。所以 HTTP 那侧保留了一条长间隔（streamSafetyInterval）
// 的兜底轮询——正确性不依赖广播器，广播器只负责把延迟从 300ms 降到 ~0 并
// 把查询量降一个数量级。真要跨副本实时，下一步是把 Publish 换成 Postgres
// 的 LISTEN/NOTIFY 或 Redis pub/sub，订阅侧的接口不用动。
package runstream

import (
	"sync"
	"sync/atomic"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// subscriptionBuffer 是单个订阅者的积压上限。
//
// 取 512 是按"最慢的消费者"估的：一次 flush 之间攒下的 token 级事件远不到
// 这个量级。超了也不阻塞发布方——见 Publish 里的 default 分支。
const subscriptionBuffer = 512

// Broker 把一次运行的事件扇出给当前正在看这次运行的所有连接。
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[*Subscription]struct{}
	// acc 攒着每个节点"到目前为止"的文字。逐 token 的增量不落库之后，中途
	// 接入的客户端要靠它恢复现场——见 accumulator 的注释。
	acc *accumulator
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[*Subscription]struct{}), acc: newAccumulator()}
}

// Snapshot 返回这次运行当前的现场：每个节点已经生成了哪些正文和思维链，
// 一个节点一条 node.snapshot。SSE 处理器在补完历史之后下发它，刷新页面的
// 人因此能接着当前状态往下看，而不是空着等这一轮结束。
func (b *Broker) Snapshot(runID string) []run.Event { return b.acc.snapshot(runID) }

// Subscription 是一条连接对某次运行的订阅。用完必须 Close，否则 broker 里
// 会一直留着它的 channel。
type Subscription struct {
	broker *Broker
	runID  string
	ch     chan run.Event
	// lagged 记录"曾经因为这个订阅者太慢而丢过事件"。消费方读到它就知道
	// 手上的实时流有洞，得回库补一次——丢事件本身不可怕，假装没丢才可怕。
	lagged    atomic.Bool
	closeOnce sync.Once
}

// Subscribe 必须在首次读库**之前**调用。反过来的话，两步之间引擎写入的事
// 件既不在首次查询结果里、也不在订阅流里，会被永久跳过。
func (b *Broker) Subscribe(runID string) *Subscription {
	s := &Subscription{broker: b, runID: runID, ch: make(chan run.Event, subscriptionBuffer)}
	b.mu.Lock()
	defer b.mu.Unlock()
	set := b.subs[runID]
	if set == nil {
		set = make(map[*Subscription]struct{})
		b.subs[runID] = set
	}
	set[s] = struct{}{}
	return s
}

// Publish 把一个已落库的事件扇出给该运行的订阅者。
//
// 绝不阻塞：发布方是执行引擎所在的 goroutine，一个卡住的浏览器连接不能把
// 整次运行拖停。channel 满了就丢事件并给订阅者打上 lagged 标记，由消费方
// 回库补齐。
func (b *Broker) Publish(ev run.Event) {
	b.mu.Lock()
	subs := make([]*Subscription, 0, len(b.subs[ev.RunID]))
	for s := range b.subs[ev.RunID] {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		select {
		case s.ch <- ev:
		default:
			s.lagged.Store(true)
		}
	}
}

// Events 是实时事件流。事件按 id 升序到达（发布顺序即 INSERT 顺序）。
//
// channel 不会被 Close 关闭：关闭它会和 Publish 的发送撞成 panic。生命周
// 期由 Close 从 broker 摘除订阅来结束，剩下的交给 GC。
func (s *Subscription) Events() <-chan run.Event { return s.ch }

// Lagged 报告并清除积压标记。返回 true 表示这条实时流丢过事件，调用方必须
// 回库补读，不能只靠 channel 里剩下的内容。
func (s *Subscription) Lagged() bool { return s.lagged.Swap(false) }

func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		b := s.broker
		b.mu.Lock()
		defer b.mu.Unlock()
		set := b.subs[s.runID]
		delete(set, s)
		if len(set) == 0 {
			delete(b.subs, s.runID)
		}
	})
}

// SubscriberCount 报告当前有多少条连接在看这次运行。给测试用来确认订阅已
// 经挂上（推流是异步的，推早了就推给了一个还不存在的订阅者）。
func (b *Broker) SubscriberCount(runID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[runID])
}
