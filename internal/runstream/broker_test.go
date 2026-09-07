package runstream

import (
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

func TestBroker_FansOutToEverySubscriberOfThatRun(t *testing.T) {
	b := NewBroker()
	a, c := b.Subscribe("run-1"), b.Subscribe("run-1")
	other := b.Subscribe("run-2")
	defer func() { a.Close(); c.Close(); other.Close() }()

	b.Publish(run.Event{ID: 7, RunID: "run-1", Type: "node.thinking"})

	for i, sub := range []*Subscription{a, c} {
		select {
		case ev := <-sub.Events():
			if ev.ID != 7 {
				t.Fatalf("订阅者 %d 收到了错的事件：%+v", i, ev)
			}
		default:
			t.Fatalf("订阅者 %d 什么都没收到", i)
		}
	}
	select {
	case ev := <-other.Events():
		t.Fatalf("另一次运行的订阅者不该收到这条事件：%+v", ev)
	default:
	}
}

// 慢消费者不能把发布方（执行引擎）堵住。channel 满了就丢事件，但必须留下
// lagged 标记——HTTP 那层靠它知道"手上的实时流有洞，得回库补"。默默丢事件
// 才是真正的 bug：前端会永久少几个 token 而没有任何人发现。
func TestBroker_SlowSubscriberIsMarkedLaggedInsteadOfBlocking(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run-1")
	defer sub.Close()

	for i := 0; i < subscriptionBuffer+10; i++ {
		b.Publish(run.Event{ID: int64(i + 1), RunID: "run-1", Type: "node.thinking"})
	}

	if !sub.Lagged() {
		t.Fatal("溢出之后必须标记 lagged")
	}
	if sub.Lagged() {
		t.Fatal("Lagged 读一次就该清零，否则每一轮都会白白回库")
	}
}

// Close 之后再发布不能 panic，也不能把已经走掉的连接留在 broker 里。
func TestBroker_PublishAfterCloseIsSafe(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run-1")
	sub.Close()
	sub.Close() // 幂等：defer 和显式关闭撞上是常态

	b.Publish(run.Event{ID: 1, RunID: "run-1", Type: run.EventBundleFinished})

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) != 0 {
		t.Fatalf("Close 之后 broker 里不该还留着订阅：%+v", b.subs)
	}
}
