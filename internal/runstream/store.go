package runstream

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// PublishingStore 是"落库 + 推流"两条线的接缝：它包住真正的 EventStore，
// 在 INSERT 成功之后、把事件回给调用方之前，顺手推给广播器。
//
// 顺序是刻意的——先落库再推：
//
//   - 事件的 id 是数据库分配的，推流侧要用它给订阅者去重、给断线重连当
//     游标。先推就没有 id 可推。
//   - 推出去的每一条都保证已经持久化。反过来（先推后写）会让前端看到一
//     条刷新后就消失的事件。
//
// 写失败则完全不推：宁可让实时流少一条（兜底轮询会发现并补上），也不能推
// 一条数据库里不存在的事件出去。
type PublishingStore struct {
	inner run.EventStore
	bus   *Broker
}

func NewPublishingStore(inner run.EventStore, bus *Broker) *PublishingStore {
	return &PublishingStore{inner: inner, bus: bus}
}

func (s *PublishingStore) Append(ctx context.Context, ev run.Event) (run.Event, error) {
	stored, err := s.inner.Append(ctx, ev)
	if err != nil {
		return stored, err
	}
	if s.bus != nil {
		s.bus.Publish(stored)
	}
	return stored, nil
}

func (s *PublishingStore) ListAfter(ctx context.Context, runID string, afterID int64) ([]run.Event, error) {
	return s.inner.ListAfter(ctx, runID, afterID)
}

// 编译期确认装饰器和被装饰者是同一个接口。
var _ run.EventStore = (*PublishingStore)(nil)
