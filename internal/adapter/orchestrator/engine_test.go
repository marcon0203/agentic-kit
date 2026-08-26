package orchestrator

import (
	"reflect"
	"sort"
	"testing"

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
