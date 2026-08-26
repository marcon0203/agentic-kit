package adk

import (
	"regexp"
	"strings"
)

// RendererRegistration is one plugin renderer an Agent's capabilities.tools[]
// authorized for this run (spec-20 §4.2). Two distinct sources feed the same
// struct: an explicit tools[].ui entry (TriggerTool set, FencedLangs nil —
// "the model called this tool, render its result") and a renderers[]
// auto_render declaration (FencedLangs set, TriggerTool empty — "this
// output pattern gets taken over without the model knowing").
type RendererRegistration struct {
	PluginID     string
	Version      string
	RendererName string
	OSSPrefix    string
	// Entry is the package-relative path to the renderer's static HTML
	// entry point (renderers[].entry, or the tools[].ui path for an
	// explicit registration) — what GET /plugins/assets/{id}/{ver}/*
	// serves at load time; the iframe's other JS/CSS assets are fetched
	// relative to it from the same endpoint.
	Entry string
	// FencedLangs is non-empty for an auto_render registration: the fenced
	// code-block languages (renderers[].auto_render.fenced_lang) that hand
	// this node's output over to this renderer.
	FencedLangs []string
	// TriggerTool is non-empty for an explicit tools[].ui registration: the
	// tool name whose successful call hands its result to this renderer.
	TriggerTool string
	// Description is the renderers[] entry's own output-format explanation
	// (schemas/plugin.schema.json's renderers[].description) — the model
	// has no other way to learn what shape an auto_render fenced block
	// needs (there's no input_schema the way a tool call has one), so
	// CompileAgent appends this to the agent's persona for every
	// auto_render registration. Empty for an explicit tools[].ui
	// registration, whose format is already covered by the tool's own
	// description/input_schema.
	Description string
}

// ResourceURI is what the frontend uses to fetch this renderer's iframe
// content (spec-20 §4.2's node.render event payload).
func (r RendererRegistration) ResourceURI() string {
	return "ui://" + r.PluginID + "/" + r.RendererName
}

// Plugin renderer ToolSpec.Config keys the Authorizer populates when a ref
// resolves to a renderers[] entry (KindPluginRenderer) rather than a
// tools[] one.
const (
	PluginRendererConfigKeyPluginID    = "renderer_plugin_id"
	PluginRendererConfigKeyVersion     = "renderer_version"
	PluginRendererConfigKeyName        = "renderer_name"
	PluginRendererConfigKeyOSSPrefix   = "renderer_oss_prefix"
	PluginRendererConfigKeyEntry       = "renderer_entry"
	PluginRendererConfigKeyDescription = "renderer_description"
	PluginRendererConfigKeyFencedLangs = "renderer_fenced_langs" // []string
)

// RendererRegistrationFromRendererSpec builds a RendererRegistration from a
// KindPluginRenderer ToolSpec — an auto_render registration.
func RendererRegistrationFromRendererSpec(spec ToolSpec) (RendererRegistration, bool) {
	pluginID, _ := spec.Config[PluginRendererConfigKeyPluginID].(string)
	name, _ := spec.Config[PluginRendererConfigKeyName].(string)
	if pluginID == "" || name == "" {
		return RendererRegistration{}, false
	}
	version, _ := spec.Config[PluginRendererConfigKeyVersion].(string)
	ossPrefix, _ := spec.Config[PluginRendererConfigKeyOSSPrefix].(string)
	entry, _ := spec.Config[PluginRendererConfigKeyEntry].(string)
	description, _ := spec.Config[PluginRendererConfigKeyDescription].(string)
	fencedLangs, _ := spec.Config[PluginRendererConfigKeyFencedLangs].([]string)
	return RendererRegistration{
		PluginID: pluginID, Version: version, RendererName: name,
		OSSPrefix: ossPrefix, Entry: entry, Description: description, FencedLangs: fencedLangs,
	}, true
}

// RendererRegistrationFromToolSpec builds a RendererRegistration from a
// KindPlugin (callable tool) ToolSpec whose manifest declared tools[].ui
// (spec-20 §4.2 method A: the model's own call to toolName is the trigger).
func RendererRegistrationFromToolSpec(spec ToolSpec, toolName string) (RendererRegistration, bool) {
	uiEntry, _ := spec.Config[PluginConfigKeyUIEntry].(string)
	if uiEntry == "" {
		return RendererRegistration{}, false
	}
	wasmKey, _ := spec.Config[PluginConfigKeyWasmKey].(string)
	pluginID, version, _ := strings.Cut(wasmKey, "@")
	ossPrefix, _ := spec.Config[PluginRendererConfigKeyOSSPrefix].(string)
	return RendererRegistration{
		PluginID: pluginID, Version: version, RendererName: toolName,
		OSSPrefix: ossPrefix, Entry: uiEntry, TriggerTool: toolName,
	}, true
}

// fencedBlockPattern matches a fenced code block and captures its
// language tag and body — good enough to find "```lang\n...\n```" without
// pulling in a full markdown parser, which is all auto_render needs.
var fencedBlockPattern = regexp.MustCompile("(?s)```([A-Za-z0-9_-]+)\\n(.*?)```")

// MatchAutoRender finds the first registration (in declaration order —
// spec-20 §4.2's "先声明者赢" tiebreak) whose FencedLangs matches a fenced
// code block actually present in text, and returns that block's content.
// Registrations with no FencedLangs (explicit tools[].ui entries) are
// never matched here — MatchToolRender handles those.
func MatchAutoRender(text string, regs []RendererRegistration) (reg RendererRegistration, lang, content string, matched bool) {
	blocks := fencedBlockPattern.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		return RendererRegistration{}, "", "", false
	}
	for _, r := range regs {
		if len(r.FencedLangs) == 0 {
			continue
		}
		for _, block := range blocks {
			blockLang, blockContent := block[1], block[2]
			for _, want := range r.FencedLangs {
				if blockLang == want {
					return r, blockLang, blockContent, true
				}
			}
		}
	}
	return RendererRegistration{}, "", "", false
}

// MatchToolRender finds the registration an explicit tools[].ui entry
// registered for toolName (method A, spec-20 §4.2) — the successful call
// itself is the trigger, no output pattern to match.
func MatchToolRender(toolName string, regs []RendererRegistration) (RendererRegistration, bool) {
	for _, r := range regs {
		if r.TriggerTool != "" && r.TriggerTool == toolName {
			return r, true
		}
	}
	return RendererRegistration{}, false
}
