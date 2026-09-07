package api

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
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
