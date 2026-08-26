package adk

import "testing"

func TestMatchAutoRender_FirstDeclaredWins(t *testing.T) {
	regs := []RendererRegistration{
		{PluginID: "acme.charts", RendererName: "chart", FencedLangs: []string{"chart"}},
		{PluginID: "other.viz", RendererName: "chart2", FencedLangs: []string{"chart"}},
	}
	text := "here you go:\n```chart\n{\"type\":\"bar\"}\n```\nhope that helps"

	reg, lang, content, matched := MatchAutoRender(text, regs)
	if !matched {
		t.Fatal("expected a match")
	}
	if reg.PluginID != "acme.charts" {
		t.Fatalf("expected the first-declared registration to win, got %q", reg.PluginID)
	}
	if lang != "chart" {
		t.Fatalf("unexpected lang: %q", lang)
	}
	if content != "{\"type\":\"bar\"}\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestMatchAutoRender_NoFencedBlock_NoMatch(t *testing.T) {
	regs := []RendererRegistration{{PluginID: "acme.charts", FencedLangs: []string{"chart"}}}
	_, _, _, matched := MatchAutoRender("plain text, no code block", regs)
	if matched {
		t.Fatal("expected no match for text with no fenced block")
	}
}

func TestMatchAutoRender_WrongLang_NoMatch(t *testing.T) {
	regs := []RendererRegistration{{PluginID: "acme.charts", FencedLangs: []string{"chart"}}}
	_, _, _, matched := MatchAutoRender("```json\n{}\n```", regs)
	if matched {
		t.Fatal("expected no match when the fenced block's lang isn't declared")
	}
}

func TestMatchAutoRender_SkipsExplicitOnlyRegistrations(t *testing.T) {
	// A TriggerTool-only registration (no FencedLangs) must never match
	// auto_render — that's MatchToolRender's job.
	regs := []RendererRegistration{{PluginID: "acme.charts", RendererName: "chart", TriggerTool: "render_chart"}}
	_, _, _, matched := MatchAutoRender("```chart\n{}\n```", regs)
	if matched {
		t.Fatal("expected an explicit-trigger-only registration to never match auto_render")
	}
}

func TestMatchToolRender_FindsRegisteredTool(t *testing.T) {
	regs := []RendererRegistration{
		{PluginID: "acme.charts", RendererName: "chart", TriggerTool: "acme_charts_render_chart"},
	}
	reg, ok := MatchToolRender("acme_charts_render_chart", regs)
	if !ok {
		t.Fatal("expected a match")
	}
	if reg.RendererName != "chart" {
		t.Fatalf("unexpected renderer: %q", reg.RendererName)
	}
}

func TestMatchToolRender_NoMatchForUnregisteredTool(t *testing.T) {
	regs := []RendererRegistration{{TriggerTool: "acme_charts_render_chart"}}
	if _, ok := MatchToolRender("some_other_tool", regs); ok {
		t.Fatal("expected no match for an unregistered tool name")
	}
}

func TestRendererRegistration_ResourceURI(t *testing.T) {
	reg := RendererRegistration{PluginID: "acme.charts", RendererName: "chart"}
	if got := reg.ResourceURI(); got != "ui://acme.charts/chart" {
		t.Fatalf("unexpected resource_uri: %q", got)
	}
}

func TestRendererRegistrationFromRendererSpec(t *testing.T) {
	spec := ToolSpec{Kind: KindPluginRenderer, Config: map[string]any{
		PluginRendererConfigKeyPluginID:    "acme.charts",
		PluginRendererConfigKeyVersion:     "1.0.0",
		PluginRendererConfigKeyName:        "chart",
		PluginRendererConfigKeyOSSPrefix:   "plugins/acme.charts/1.0.0",
		PluginRendererConfigKeyEntry:       "ui/chart.html",
		PluginRendererConfigKeyDescription: "emit a ```chart block shaped {labels, datasets}",
		PluginRendererConfigKeyFencedLangs: []string{"chart"},
	}}
	reg, ok := RendererRegistrationFromRendererSpec(spec)
	if !ok {
		t.Fatal("expected ok")
	}
	if reg.PluginID != "acme.charts" || reg.Version != "1.0.0" || reg.Entry != "ui/chart.html" {
		t.Fatalf("unexpected registration: %+v", reg)
	}
	if reg.Description != "emit a ```chart block shaped {labels, datasets}" {
		t.Fatalf("unexpected description: %q", reg.Description)
	}
	if len(reg.FencedLangs) != 1 || reg.FencedLangs[0] != "chart" {
		t.Fatalf("unexpected fenced langs: %+v", reg.FencedLangs)
	}
}

func TestRendererRegistrationFromRendererSpec_MissingFields(t *testing.T) {
	if _, ok := RendererRegistrationFromRendererSpec(ToolSpec{Config: map[string]any{}}); ok {
		t.Fatal("expected ok=false for a spec missing plugin_id/name")
	}
}

func TestRendererRegistrationFromToolSpec(t *testing.T) {
	spec := ToolSpec{Config: map[string]any{
		PluginConfigKeyWasmKey: "acme.charts@1.0.0",
		PluginConfigKeyUIEntry: "ui/chart.html",
	}}
	reg, ok := RendererRegistrationFromToolSpec(spec, "acme_charts_render_chart")
	if !ok {
		t.Fatal("expected ok")
	}
	if reg.PluginID != "acme.charts" || reg.Version != "1.0.0" {
		t.Fatalf("unexpected plugin id/version: %+v", reg)
	}
	if reg.TriggerTool != "acme_charts_render_chart" {
		t.Fatalf("unexpected trigger tool: %q", reg.TriggerTool)
	}
}

func TestRendererRegistrationFromToolSpec_NoUIEntry(t *testing.T) {
	spec := ToolSpec{Config: map[string]any{PluginConfigKeyWasmKey: "acme.charts@1.0.0"}}
	if _, ok := RendererRegistrationFromToolSpec(spec, "some_tool"); ok {
		t.Fatal("expected ok=false when the spec has no ui entry")
	}
}
