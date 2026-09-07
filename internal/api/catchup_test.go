package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/runstream"
)

// 复现用户观察到的场景：连上来时库里已经堆了几百条历史，补读完之后运行
// 仍在继续。补历史那一下会不会把实时那条路带坏？
func TestStream_LiveDeliveryStaysSmoothAfterALargeCatchUp(t *testing.T) {
	const backlog = 675

	f := newRunFixture()
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 5}
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}
	for i := 1; i <= backlog; i++ {
		f.events.append(run.Event{
			ID: int64(i), RunID: "run-1", Type: "node.thinking", Node: "w",
			Payload: map[string]any{"text": "x"},
		})
	}

	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handlers.Stream(w, runRequest(http.MethodGet, r.URL.String(), "run-1", 5, nil))
	})
	h = LoggingMiddleware(logger)(h)
	h = RecoverMiddleware(logger)(h)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 补历史之后，按 50ms 一条继续推 20 条实时事件。
	go func() {
		waitForSubscriber(t, f.bus, "run-1")
		for i := backlog + 1; i <= backlog+20; i++ {
			ev := run.Event{ID: int64(i), RunID: "run-1", Type: "node.thinking", Node: "w",
				Payload: map[string]any{"text": "y"}}
			f.events.append(ev) // 引擎先落库
			f.bus.Publish(ev)   // 再推流，和 runstream.PublishingStore 同序
			time.Sleep(50 * time.Millisecond)
		}
		f.bus.Publish(run.Event{ID: 99999, RunID: "run-1", Type: run.EventBundleFinished})
	}()

	resp, err := srv.Client().Get(srv.URL + "/runs/run-1/stream")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	start := time.Now()
	var arrivals []time.Duration
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			arrivals = append(arrivals, time.Since(start))
		}
	}
	if len(arrivals) < backlog+20 {
		t.Fatalf("行数不够：%d", len(arrivals))
	}

	// 补历史之后那 20 条实时事件，相邻到达间隔应当都在 50ms 上下。
	// 只要有一条等了超过 1 秒，就说明实时推送没接上、退化成了兜底轮询。
	var worst time.Duration
	live := arrivals[backlog:]
	for i := 1; i < len(live); i++ {
		if gap := live[i] - live[i-1]; gap > worst {
			worst = gap
		}
	}
	fmt.Printf("补历史 %d 条后：实时段 %d 条，最大相邻间隔 %v\n", backlog, len(live), worst.Round(time.Millisecond))
	if worst > 1*time.Second {
		t.Fatalf("实时推送退化成了轮询：最大间隔 %v", worst)
	}
}

// 刷新页面后接着当前状态往下看。
//
// 这是"逐 token 增量不落库"之后唯一需要额外设计的一块：库里读不到已经生成
// 的那半段文字，所以 SSE 处理器在补完历史之后要下发一条 node.snapshot 把
// 现场补上。没有它，刷新页面的人会看到答案凭空少一截、只能空等这一轮结束。
func TestStream_MidRunReconnectGetsTheSnapshotAndKeepsFollowing(t *testing.T) {
	f := newRunFixture()
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 5}
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}

	// 运行已经跑了一会儿：用户原话落了库，模型吐的字只在累积缓冲里。
	store := runstream.NewPublishingStore(f.events, f.bus)
	ctx := context.Background()
	if _, err := store.Append(ctx, run.Event{RunID: "run-1", Type: run.EventBundleStarted,
		Payload: map[string]any{"input": map[string]any{"message": "你好"}}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	for _, c := range []string{"今", "天", "天", "气"} {
		if _, err := store.Append(ctx, run.Event{RunID: "run-1", Type: run.EventNodeThinking,
			Node: "w", Payload: map[string]any{"text": c}}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handlers.Stream(w, runRequest(http.MethodGet, r.URL.String(), "run-1", 5, nil))
	})
	h = LoggingMiddleware(logger)(h)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 刷新页面 = 一条全新的连接，从头（after_id=0）接上来。
	go func() {
		waitForSubscriber(t, f.bus, "run-1")
		for _, c := range []string{"不", "错"} {
			_, _ = store.Append(ctx, run.Event{RunID: "run-1", Type: run.EventNodeThinking,
				Node: "w", Payload: map[string]any{"text": c}})
		}
		_, _ = store.Append(ctx, run.Event{RunID: "run-1", Type: run.EventNodeFinished,
			Node: "w", Payload: map[string]any{"text": "今天天气不错"}})
		_, _ = store.Append(ctx, run.Event{RunID: "run-1", Type: run.EventBundleFinished})
	}()

	resp, err := srv.Client().Get(srv.URL + "/runs/run-1/stream")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got []runEventDTO
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var dto runEventDTO
		if err := json.Unmarshal(sc.Bytes(), &dto); err != nil {
			t.Fatalf("解析 NDJSON 失败: %v", err)
		}
		got = append(got, dto)
	}

	// 第一条是历史（用户原话），第二条必须是补现场的快照。
	if len(got) < 2 || got[0].Type != run.EventBundleStarted {
		t.Fatalf("第一条应当是 bundle.started，实际 %+v", got)
	}
	snap := got[1]
	if snap.Type != run.EventNodeSnapshot {
		t.Fatalf("补完历史之后应当下发 node.snapshot，实际是 %s", snap.Type)
	}
	if txt, _ := snap.Payload["text"].(string); txt != "今天天气" {
		t.Fatalf("快照没带上刷新前已经生成的文字：%q", txt)
	}
	if snap.ID != 0 {
		t.Fatalf("快照没有落库，id 必须是 0（否则会污染断线续传游标），实际 %d", snap.ID)
	}

	// 而且接得上：快照之后继续收到实时增量，直到这一轮的终态。
	var live []string
	var sawFinished bool
	for _, e := range got[2:] {
		if e.Type == run.EventNodeThinking {
			if txt, _ := e.Payload["text"].(string); txt != "" {
				live = append(live, txt)
			}
		}
		if e.Type == run.EventBundleFinished {
			sawFinished = true
		}
	}
	if strings.Join(live, "") != "不错" {
		t.Fatalf("快照之后应当继续收到实时增量，实际 %v", live)
	}
	if !sawFinished {
		t.Fatal("应当一直跟到 bundle.finished")
	}
}
