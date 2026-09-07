package runstream

import (
	"context"
	"sort"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// PublishingStore 是"落库 + 推流"两条线的接缝，也是"什么算历史"这条策略的
// 唯一所在地。引擎照常对每一条事件调 Append，由这里决定它的去向。
//
// 分工：
//
//   - **历史 = 消息**。用户原话、完整答案、思维链、工具调用、生命周期
//     事件落库，一次运行大约十行。这是刷新页面、看运行详情、回放执行过程
//     时读的东西。
//   - **传输 = token**。node.thinking / node.reasoning 的逐字增量只推给正
//     在看这次运行的连接，不进数据库。以前它们是一条 delta 一行，一次运行
//     能写上千行 {"text":"可"}，而回放时一个字都不贡献——节点跑完后
//     node.finished 携带的是完整正文，timeline 对它是赋值不是追加。
//
// 增量不落库之后，"中途刷新页面还能接着看"由 accumulator 兜住：它攒着每个
// 节点到目前为止的文字，客户端接入时合成一条 node.snapshot 下发。
//
// 落库顺序上有一条不变量：**同一个节点的思维链必须排在它的 node.finished
// 之前**。前端按事件顺序渲染"先想后答"，反过来会让思考过程出现在答案下面。
type PublishingStore struct {
	inner run.EventStore
	bus   *Broker
}

func NewPublishingStore(inner run.EventStore, bus *Broker) *PublishingStore {
	return &PublishingStore{inner: inner, bus: bus}
}

// acc 是广播器持有的那个累积器——"一次运行的实时状态"由 Broker 一个对象
// 拿着，SSE 处理器因此只需要认识 Broker。
func (s *PublishingStore) acc() *accumulator { return s.bus.acc }

func (s *PublishingStore) Append(ctx context.Context, ev run.Event) (run.Event, error) {
	switch {
	case run.IsEphemeralType(ev.Type):
		// 只推不存。累积一份供中途接入的人恢复现场。
		s.acc().add(ev)
		ev.Ephemeral = true
		s.publish(ev)
		return ev, nil

	case ev.Type == run.EventNodeFinished:
		// 这个节点跑完了。正文已经由 node.finished 完整承载，攒的增量可以
		// 丢；思维链不会被它覆盖，落成一条完整的。
		_, reasoning := s.acc().takeNode(ev.RunID, ev.Node)
		if reasoning != "" {
			if _, err := s.persist(ctx, run.Event{
				RunID: ev.RunID, Type: run.EventNodeReasoning, Node: ev.Node,
				Payload: map[string]any{"text": reasoning},
			}); err != nil {
				return run.Event{}, err
			}
		}
		return s.persist(ctx, ev)

	case ev.Type == run.EventBundleFinished || ev.Type == run.EventBundleFailed:
		// 运行收尾。还没被 node.finished 收走的累积，说明那些节点没有跑到
		// 完整答案（失败、超时、被取消）——此时增量就是仅存的部分答案，落
		// 成一条快照留着，而不是连同垃圾一起丢掉。
		if err := s.flushRun(ctx, ev.RunID); err != nil {
			return run.Event{}, err
		}
		return s.persist(ctx, ev)

	default:
		return s.persist(ctx, ev)
	}
}

// flushRun 把一次运行剩下的累积各落一条 node.snapshot。
func (s *PublishingStore) flushRun(ctx context.Context, runID string) error {
	pending := s.acc().takeRun(runID)
	// map 遍历顺序随机，但事件 id 决定回放顺序——按节点名排序，让同样的一
	// 次运行每次落库的顺序都一样。
	names := make([]string, 0, len(pending))
	for node := range pending {
		names = append(names, node)
	}
	sort.Strings(names)

	for _, node := range names {
		nt := pending[node]
		if len(nt.text) == 0 && len(nt.reasoning) == 0 {
			continue
		}
		if _, err := s.persist(ctx, run.Event{
			RunID: runID, Type: run.EventNodeSnapshot, Node: node,
			Payload: map[string]any{"text": string(nt.text), "reasoning": string(nt.reasoning)},
		}); err != nil {
			return err
		}
	}
	return nil
}

// persist 落库后推流。顺序是刻意的：id 由数据库分配，推流侧要用它给订阅者
// 去重、给断线重连当游标；而且推出去的每一条都保证已经持久化。写失败则完全
// 不推——宁可让实时流少一条（兜底轮询会补上），也不推一条库里不存在的事件。
func (s *PublishingStore) persist(ctx context.Context, ev run.Event) (run.Event, error) {
	stored, err := s.inner.Append(ctx, ev)
	if err != nil {
		return stored, err
	}
	s.publish(stored)
	return stored, nil
}

func (s *PublishingStore) publish(ev run.Event) {
	if s.bus != nil {
		s.bus.Publish(ev)
	}
}

func (s *PublishingStore) ListAfter(ctx context.Context, runID string, afterID int64) ([]run.Event, error) {
	return s.inner.ListAfter(ctx, runID, afterID)
}

var _ run.EventStore = (*PublishingStore)(nil)
