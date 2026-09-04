package orchestrator

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// TestRendererFencedLangs_DedupesAcrossRegistrationsForSameNode is the
// backend half of the regression test for "先输出原始 JSON 再渲染，体验很
// 差": the frontend needs to know, before any node.thinking text arrives,
// which fenced languages on which nodes are about to be taken over by a
// renderer — otherwise it has no way to hide a ```chart block's raw JSON
// from the live-streamed chat text while it's still being typed out.
func TestRendererFencedLangs_DedupesAcrossRegistrationsForSameNode(t *testing.T) {
	rules := map[string][]adk.RendererRegistration{
		"analyst": {
			{RendererName: "chart", FencedLangs: []string{"chart"}},
			{RendererName: "chart-alt", FencedLangs: []string{"chart", "acme-chart"}},
		},
	}
	got := rendererFencedLangs(rules)
	sort.Strings(got["analyst"])
	want := []string{"acme-chart", "chart"}
	if !reflect.DeepEqual(got["analyst"], want) {
		t.Fatalf("rendererFencedLangs = %+v, want %+v", got["analyst"], want)
	}
}

func TestRendererFencedLangs_SkipsNodesWithNoFencedLangs(t *testing.T) {
	// A tools[].ui registration (TriggerTool set, FencedLangs empty) never
	// contributes — its result was never streamed as chat text to begin
	// with, so there's nothing for the frontend to hide.
	rules := map[string][]adk.RendererRegistration{
		"sales": {{RendererName: "chart", TriggerTool: "render_chart"}},
	}
	got := rendererFencedLangs(rules)
	if len(got) != 0 {
		t.Fatalf("expected no entries, got %+v", got)
	}
}

func TestRendererFencedLangs_EmptyInput_ReturnsEmptyMap(t *testing.T) {
	got := rendererFencedLangs(nil)
	if len(got) != 0 {
		t.Fatalf("expected an empty map, got %+v", got)
	}
}

// ── finish 的写入顺序 ─────────────────────────────────────────────

// recordingRuns/recordingEvents 只记调用顺序，别的什么都不做。
type recordingRuns struct{ log *[]string }

func (r recordingRuns) Create(context.Context, run.Run) (run.Run, error) { return run.Run{}, nil }
func (r recordingRuns) Get(context.Context, string) (run.Run, error)     { return run.Run{}, nil }
func (r recordingRuns) ListPage(context.Context, run.ListQuery) ([]run.Run, error) {
	return nil, nil
}
func (r recordingRuns) ListInSession(context.Context, int64, string) ([]run.Run, error) {
	return nil, nil
}
func (r recordingRuns) ListConversations(context.Context, int64, int64, int) ([]run.Conversation, error) {
	return nil, nil
}
func (r recordingRuns) UpdateStatus(_ context.Context, _ string, status run.Status, _ string) error {
	*r.log = append(*r.log, "status:"+string(status))
	return nil
}
func (r recordingRuns) MarkCancelRequested(context.Context, string) error      { return nil }
func (r recordingRuns) AddUsage(context.Context, string, int64, float64) error { return nil }

type recordingEvents struct{ log *[]string }

func (e recordingEvents) Append(_ context.Context, ev run.Event) error {
	*e.log = append(*e.log, "event:"+ev.Type)
	return nil
}

func (e recordingEvents) ListAfter(context.Context, string, int64, bool) ([]run.Event, error) {
	return nil, nil
}

// finish 必须先落终态事件再改运行状态。反过来的话，NDJSON 流刚好在这中间
// 轮询一次就会读到"已完成"而 bundle.finished 还没落库，于是断在终态事件之
// 前——前端永远等不到完成，输入框卡死在"运行中"。见 finish 的注释。
func TestFinish_AppendsTerminalEventBeforeFlippingStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    run.Status
		errMsg    string
		wantEvent string
	}{
		{"成功", run.StatusFinished, "", "event:" + run.EventBundleFinished},
		{"失败", run.StatusFailed, "boom", "event:" + run.EventBundleFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			x := &execution{
				runID:  "run-1",
				engine: &Engine{runs: recordingRuns{log: &log}, events: recordingEvents{log: &log}},
			}
			x.finish(context.Background(), tc.status, tc.errMsg)

			want := []string{tc.wantEvent, "status:" + string(tc.status)}
			if len(log) != 2 || log[0] != want[0] || log[1] != want[1] {
				t.Fatalf("调用顺序 = %v，期望 %v（事件必须先于状态）", log, want)
			}
		})
	}
}
