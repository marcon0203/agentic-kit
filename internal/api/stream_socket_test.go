package api

import (
	"bufio"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// 真实 socket 上的流式验证。
//
// 其它 Stream 用例走的是 httptest.NewRecorder——那是个内存 buffer，既不
// 走 chunked 编码，也不真的 flush，所以"handler 以为自己在流式推送、实际
// 被某一层缓冲住了"这类问题它一个都发现不了。这里用真的 HTTP server + 真
// 的连接，并且套上和线上同一条中间件链（chi 的 WrapResponseWriter 会把
// ResponseWriter 换掉，如果换出来的东西不实现 http.Flusher，flush 就全部
// 静默失效）。
func TestStream_ArrivesIncrementallyOverARealSocket(t *testing.T) {
	f := newRunFixture()
	f.resolver.bundle = run.ResolvedBundle{BundleID: 1, OwnerUserID: 5}
	f.runs.runs["run-1"] = run.Run{ID: "run-1", BundleID: 1, TriggeredBy: 5, Status: run.StatusRunning}

	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handlers.Stream(w, runRequest(http.MethodGet, r.URL.String(), "run-1", 5, nil))
	})
	h = LoggingMiddleware(logger)(h)
	h = RequestIDMiddleware(h)
	h = RecoverMiddleware(logger)(h)

	srv := httptest.NewServer(h)
	defer srv.Close()

	go func() {
		waitForSubscriber(t, f.bus, "run-1")
		for i := 1; i <= 5; i++ {
			f.bus.Publish(run.Event{ID: int64(i), RunID: "run-1", Type: "node.thinking",
				Node: "writer", Payload: map[string]any{"text": "x"}})
			time.Sleep(150 * time.Millisecond)
		}
		f.bus.Publish(run.Event{ID: 99, RunID: "run-1", Type: run.EventBundleFinished})
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
		if len(sc.Bytes()) == 0 {
			continue
		}
		arrivals = append(arrivals, time.Since(start))
	}
	if len(arrivals) < 6 {
		t.Fatalf("期望 6 行（5 条增量 + 终态），实际 %d 行", len(arrivals))
	}
	for i, at := range arrivals {
		t.Logf("第 %d 行到达于 %v", i+1, at.Round(time.Millisecond))
	}
	// 真流式的话，最后一行应当明显晚于第一行（5 × 150ms ≈ 750ms）。
	// 被缓冲的话所有行会在同一瞬间一起到达。
	spread := arrivals[len(arrivals)-1] - arrivals[0]
	if spread < 400*time.Millisecond {
		t.Fatalf("所有行几乎同时到达（跨度 %v）——说明中间有一层把响应缓冲住了，不是真流式", spread)
	}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
