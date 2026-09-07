package runstream

import (
	"context"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

type fakeStore struct {
	rows []run.Event
	next int64
}

func (f *fakeStore) Append(_ context.Context, ev run.Event) (run.Event, error) {
	f.next++
	ev.ID = f.next
	f.rows = append(f.rows, ev)
	return ev, nil
}

func (f *fakeStore) ListAfter(_ context.Context, _ string, afterID int64) ([]run.Event, error) {
	var out []run.Event
	for _, r := range f.rows {
		if r.ID > afterID {
			out = append(out, r)
		}
	}
	return out, nil
}

func newStore() (*PublishingStore, *fakeStore, *Broker) {
	inner, bus := &fakeStore{}, NewBroker()
	return NewPublishingStore(inner, bus), inner, bus
}

func thinking(text string) run.Event {
	return run.Event{RunID: "r", Type: run.EventNodeThinking, Node: "w", Payload: map[string]any{"text": text}}
}
func reasoning(text string) run.Event {
	return run.Event{RunID: "r", Type: run.EventNodeReasoning, Node: "w", Payload: map[string]any{"text": text}}
}

func types(rows []run.Event) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Type
	}
	return out
}

// 逐 token 的增量不进数据库。一次运行以前能写上千行 {"text":"可"}，而回放
// 时它们一个字都不贡献——node.finished 携带的是完整正文。
func TestPublishingStore_TokenDeltasAreNotPersisted(t *testing.T) {
	s, inner, _ := newStore()
	ctx := context.Background()

	for _, c := range strings.Split("今天天气不错", "") {
		ev, err := s.Append(ctx, thinking(c))
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if !ev.Ephemeral || ev.ID != 0 {
			t.Fatalf("增量必须标记为 ephemeral 且没有数据库 id：%+v", ev)
		}
	}
	if len(inner.rows) != 0 {
		t.Fatalf("增量不该落库，实际写了 %d 行：%v", len(inner.rows), types(inner.rows))
	}
}

// 节点跑完时：正文由 node.finished 完整承载，攒的增量丢弃；思维链不会被它
// 覆盖，必须合并成一条落库，而且要排在 node.finished 之前（前端按事件顺序
// 渲染"先想后答"）。
func TestPublishingStore_CoalescesReasoningAheadOfNodeFinished(t *testing.T) {
	s, inner, _ := newStore()
	ctx := context.Background()

	for _, c := range []string{"让", "我", "想", "想"} {
		_, _ = s.Append(ctx, reasoning(c))
	}
	for _, c := range []string{"今", "天", "天", "气"} {
		_, _ = s.Append(ctx, thinking(c))
	}
	if _, err := s.Append(ctx, run.Event{
		RunID: "r", Type: run.EventNodeFinished, Node: "w",
		Payload: map[string]any{"text": "今天天气不错"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	got := types(inner.rows)
	want := []string{run.EventNodeReasoning, run.EventNodeFinished}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("落库的应当只有「合并后的思维链 + 完整答案」两行，实际 %v", got)
	}
	if txt, _ := inner.rows[0].Payload["text"].(string); txt != "让我想想" {
		t.Fatalf("思维链没有合并完整：%q", txt)
	}
	if txt, _ := inner.rows[1].Payload["text"].(string); txt != "今天天气不错" {
		t.Fatalf("答案正文应当来自 node.finished：%q", txt)
	}
}

// 运行失败、没有 node.finished 时，增量是仅存的部分答案——必须作为一条
// node.snapshot 留下来，而不是连同"垃圾"一起丢掉。这是删增量这件事唯一
// 会真的丢历史的场景。
func TestPublishingStore_KeepsPartialAnswerWhenTheRunFails(t *testing.T) {
	s, inner, _ := newStore()
	ctx := context.Background()

	for _, c := range []string{"今", "天", "天", "气"} {
		_, _ = s.Append(ctx, thinking(c))
	}
	_, _ = s.Append(ctx, reasoning("在想"))
	if _, err := s.Append(ctx, run.Event{
		RunID: "r", Type: run.EventBundleFailed, Payload: map[string]any{"error": "超时"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	got := types(inner.rows)
	if len(got) != 2 || got[0] != run.EventNodeSnapshot || got[1] != run.EventBundleFailed {
		t.Fatalf("失败运行应当留下「快照 + 失败」两行，实际 %v", got)
	}
	if txt, _ := inner.rows[0].Payload["text"].(string); txt != "今天天气" {
		t.Fatalf("部分答案丢了：%q", txt)
	}
	if r, _ := inner.rows[0].Payload["reasoning"].(string); r != "在想" {
		t.Fatalf("部分思维链丢了：%q", r)
	}
}

// 刷新页面后要能接着当前状态往下看——快照就是那个"当前状态"。
func TestBroker_SnapshotReportsWhatHasBeenGeneratedSoFar(t *testing.T) {
	s, _, bus := newStore()
	ctx := context.Background()

	for _, c := range []string{"今", "天", "天", "气"} {
		_, _ = s.Append(ctx, thinking(c))
	}
	_, _ = s.Append(ctx, reasoning("在想"))

	snap := bus.Snapshot("r")
	if len(snap) != 1 {
		t.Fatalf("期望一个节点一条快照，实际 %d 条", len(snap))
	}
	if !snap[0].Ephemeral || snap[0].Type != run.EventNodeSnapshot {
		t.Fatalf("快照事件形态不对：%+v", snap[0])
	}
	if txt, _ := snap[0].Payload["text"].(string); txt != "今天天气" {
		t.Fatalf("快照文字不对：%q", txt)
	}
	if r, _ := snap[0].Payload["reasoning"].(string); r != "在想" {
		t.Fatalf("快照思维链不对：%q", r)
	}

	// 节点跑完之后现场就该清干净，免得内存里一直挂着已结束运行的文本。
	_, _ = s.Append(ctx, run.Event{RunID: "r", Type: run.EventNodeFinished, Node: "w",
		Payload: map[string]any{"text": "今天天气不错"}})
	if len(bus.Snapshot("r")) != 0 {
		t.Fatal("节点跑完后累积应当已经清空")
	}
}

// 中文一个字三个字节，截断必须按 rune——按字节切会把一个字劈成半个，
// 快照里出现乱码。
func TestAccumulator_TruncatesByRuneNotByte(t *testing.T) {
	s, _, bus := newStore()
	long := strings.Repeat("字", maxAccumulatedChars+500)
	_, _ = s.Append(context.Background(), thinking(long))

	txt, _ := bus.Snapshot("r")[0].Payload["text"].(string)
	if strings.Contains(txt, "�") {
		t.Fatal("截断产生了乱码——说明是按字节切的")
	}
	if got := len([]rune(txt)); got != maxAccumulatedChars {
		t.Fatalf("截断后应当正好 %d 个字，实际 %d", maxAccumulatedChars, got)
	}
}
