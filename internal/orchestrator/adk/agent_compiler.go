package adk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

// CompiledAgent is one compiled Agent DSL, ready to hand to
// BundleCompileOptions.Agents. It's a type alias (not a new type) for
// ADK's own agent.Agent, so callers outside this package (the run engine,
// spec-11) can name the type without importing google.golang.org/adk
// themselves — spec-10's "所有 ADK 调用收敛在 internal/orchestrator/adk
// 包内" applies to *importing* the SDK, not to holding a value it returns.
type CompiledAgent = agent.Agent

// AgentCompileOptions carries everything CompileAgent needs beyond the raw
// DSL document: the Gateway (spec-09) to run the compiled agent's model
// through, decrypted provider credentials, and the authorizer that decides
// which capabilities.tools/skills refs actually become ADK tools.
type AgentCompileOptions struct {
	Gateway     *modelgateway.Gateway
	Credentials map[string]modelgateway.Credential // provider -> decrypted credential
	Authorizer  ResourceAuthorizer
	// KnowledgeBaseSearcher backs a "knowledge_base" resource's real vector
	// search; nil is fine when no Agent in the Bundle references one.
	KnowledgeBaseSearcher KnowledgeBaseSearcher
	// SkillContentFetcher backs a zip-uploaded "skill" resource's SKILL.md
	// fetch (spec-05a); nil is fine when OSS isn't configured or no Agent
	// references an OSS-backed Skill — buildSkillTool falls back to
	// config.instructions.
	SkillContentFetcher SkillContentFetcher
	// PluginRuntime backs a "plugin:{id}/{tool}" capabilities.tools[] ref
	// (spec-20); nil is fine when no Agent in the Bundle references one.
	PluginRuntime PluginRuntime
	// Renderers, if non-nil, receives every plugin renderer this Agent's
	// capabilities.tools[] resolved during compilation — both
	// renderers[].auto_render registrations and explicit tools[].ui ones
	// (spec-20 §4.2), in declaration order (needed for its own "先声明者赢"
	// tiebreak). The caller (the run engine, per node) uses this to match
	// node.render events against the node's actual output; CompileAgent
	// itself never emits one.
	Renderers *[]RendererRegistration
	// Hooks, if non-nil, receives every plugin hook this Agent's
	// capabilities.hooks{} resolved during compilation (spec-20 §4.4).
	// CompileAgent returns an error if two different plugins claim the
	// same hook point — there is no "first wins" arbitration for hooks
	// the way there is for renderers, because a hook can rewrite output
	// and running two in an undefined order is a correctness bug, not a
	// display quirk.
	Hooks *[]HookRegistration
}

// CompileAgent turns one validated Agent DSL document (schemas/agent.schema.json)
// into an ADK llmagent. Every capabilities.tools/skills ref is resolved
// through opts.Authorizer *before* the agent is built — an unauthorized
// ref is simply omitted from Tools rather than causing a runtime failure
// (spec-10 §2: "未授权的资源不进入图... 编译期就不构造这个 Tool").
func CompileAgent(ctx context.Context, def map[string]any, opts AgentCompileOptions) (agent.Agent, error) {
	ref, _ := def["agent"].(string)
	if ref == "" {
		return nil, fmt.Errorf("adk: agent definition missing \"agent\" ref")
	}
	role, _ := def["role"].(string)
	persona, _ := def["persona"].(string)

	primary, fallbacks, err := parseModelSpecs(def)
	if err != nil {
		return nil, fmt.Errorf("adk: agent %q: %w", ref, err)
	}
	if cred, ok := opts.Credentials[primary.Provider]; !ok || cred.APIKey == "" {
		if _, ok := anyFallbackHasKey(fallbacks, opts.Credentials); !ok {
			return nil, fmt.Errorf("adk: agent %q: %w (%s)", ref, ErrNoAPIKey, primary.Provider)
		}
	}

	var renderers []RendererRegistration
	tools, toolsets, err := compileTools(ctx, def, opts.Authorizer, opts.KnowledgeBaseSearcher, opts.SkillContentFetcher, opts.PluginRuntime, &renderers)
	if err != nil {
		return nil, fmt.Errorf("adk: agent %q: %w", ref, err)
	}
	if opts.Renderers != nil {
		*opts.Renderers = append(*opts.Renderers, renderers...)
	}
	persona = appendRendererInstructions(persona, renderers)

	var nodeHooks []HookRegistration
	if err := compileHooks(ctx, def, opts.Authorizer, &nodeHooks); err != nil {
		return nil, fmt.Errorf("adk: agent %q: %w", ref, err)
	}
	if opts.Hooks != nil {
		*opts.Hooks = append(*opts.Hooks, nodeHooks...)
	}

	llm := NewGatewayLLM(opts.Gateway, primary, fallbacks, opts.Credentials)

	// Visibility into what a run actually sends the model — without this,
	// a silently-empty tool list or a persona that never made it through is
	// invisible until someone notices the model behaving as if it had
	// neither. Persona is logged in full (not just a length) because that's
	// exactly what "试运行看不到实际 prompt" needs to debug; it's the
	// author's own agent definition, not a secret.
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		toolNames = append(toolNames, t.Name())
	}
	slog.Info("agent_compiled", "agent_ref", ref, "provider", primary.Provider, "model", primary.Name,
		"persona", persona, "tools", toolNames)

	cfg := llmagent.Config{
		Name:        ref,
		Description: role,
		Instruction: persona,
		Model:       llm,
		Tools:       tools,
		Toolsets:    toolsets,
		OutputKey:   ref,
	}
	applyHookCallbacks(&cfg, nodeHooks, opts.PluginRuntime)

	a, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("adk: agent %q: compile llmagent: %w", ref, err)
	}
	return a, nil
}

// appendRendererInstructions tells the model what an auto_render renderer
// actually expects it to write. Without this, a renderers[].auto_render
// registration (spec-20 §4.2) is entirely invisible to the model: unlike a
// tool call, there's no input_schema/description round-trip through the
// provider's function-calling API to communicate the required fenced-block
// shape — the model has to be told in the persona itself, or it just
// guesses (and, per the built-in chart renderer's own strict labels-field
// check, guesses wrong more often than not). Registrations with no
// FencedLangs (explicit tools[].ui ones) or no Description are skipped —
// their format is already covered by the tool's own declaration, or the
// plugin author didn't provide one to add.
func appendRendererInstructions(persona string, renderers []RendererRegistration) string {
	var b strings.Builder
	b.WriteString(persona)
	for _, r := range renderers {
		if len(r.FencedLangs) == 0 || r.Description == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(r.Description)
	}
	return b.String()
}

func parseModelSpecs(def map[string]any) (primary modelgateway.ModelSpec, fallbacks []modelgateway.ModelSpec, err error) {
	m, _ := def["model"].(map[string]any)
	provider, _ := m["provider"].(string)
	name, _ := m["name"].(string)
	if provider == "" || name == "" {
		return modelgateway.ModelSpec{}, nil, fmt.Errorf("missing model.provider/model.name")
	}
	primary = modelgateway.ModelSpec{Provider: provider, Name: name}

	fallbackRaw, _ := m["fallback"].([]any)
	for _, f := range fallbackRaw {
		s, _ := f.(string)
		spec, err := modelgateway.ParseModelSpec(s)
		if err != nil {
			return modelgateway.ModelSpec{}, nil, fmt.Errorf("model.fallback: %w", err)
		}
		fallbacks = append(fallbacks, spec)
	}
	return primary, fallbacks, nil
}

func anyFallbackHasKey(fallbacks []modelgateway.ModelSpec, creds map[string]modelgateway.Credential) (modelgateway.ModelSpec, bool) {
	for _, f := range fallbacks {
		if cred, ok := creds[f.Provider]; ok && cred.APIKey != "" {
			return f, true
		}
	}
	return modelgateway.ModelSpec{}, false
}

func compileTools(ctx context.Context, def map[string]any, authorizer ResourceAuthorizer, kb KnowledgeBaseSearcher, skills SkillContentFetcher, plugins PluginRuntime, renderers *[]RendererRegistration) ([]tool.Tool, []tool.Toolset, error) {
	caps, _ := def["capabilities"].(map[string]any)
	if caps == nil || authorizer == nil {
		return nil, nil, nil
	}

	var tools []tool.Tool
	var toolsets []tool.Toolset

	for _, name := range toStringList(caps["builtin_tools"]) {
		t, err := BuildBuiltinTool(name)
		if err != nil {
			return nil, nil, fmt.Errorf("builtin tool %q: %w", name, err)
		}
		tools = append(tools, t)
	}

	var refs []string
	refs = append(refs, toStringList(caps["tools"])...)
	refs = append(refs, toStringList(caps["skills"])...)

	for _, ref := range refs {
		spec, ok, err := authorizer.Authorize(ctx, ref)
		if err != nil {
			return nil, nil, fmt.Errorf("authorize %q: %w", ref, err)
		}
		if !ok {
			// Not authorized: silently omitted, per spec-10 — the model
			// simply never sees this capability, rather than failing at
			// runtime when it tries to call it.
			continue
		}
		// An MCP server's tool count isn't known until connect time, so it
		// builds a Toolset (discovered per-invocation) rather than a
		// single Tool like every other resource kind.
		if spec.Kind == KindMCP {
			ts, err := BuildMCPToolset(spec)
			if err != nil {
				return nil, nil, fmt.Errorf("build mcp toolset %q: %w", ref, err)
			}
			toolsets = append(toolsets, ts)
			continue
		}
		// A "sandbox" component (config.component_type — still Kind
		// "tool", registered the same way any other 组件 is) exposes two
		// tools (run_code, execute_command) off one Daytona sandbox, not
		// one — same reason MCP gets its own branch above.
		if componentType, _ := spec.Config[ConfigKeyComponentType].(string); componentType == ComponentTypeSandbox {
			sandboxTools, err := BuildSandboxTools(spec)
			if err != nil {
				return nil, nil, fmt.Errorf("build sandbox tools %q: %w", ref, err)
			}
			tools = append(tools, sandboxTools...)
			continue
		}
		// A "plugin:{id}/{renderer_name}" ref resolving to a renderers[]
		// entry (spec-20 §4.2) is never a callable tool — it only feeds
		// the auto_render registry the run engine matches node output
		// against.
		if spec.Kind == KindPluginRenderer {
			if renderers != nil {
				if reg, ok := RendererRegistrationFromRendererSpec(spec); ok {
					*renderers = append(*renderers, reg)
				}
			}
			continue
		}
		// A "plugin:{id}/{tool}" ref (spec-20 §5.1) — third-party WASM code,
		// not a resource-center row, but authorized and dispatched the same
		// way as everything else here.
		if spec.Kind == KindPlugin {
			t, err := BuildPluginTool(spec, plugins)
			if err != nil {
				return nil, nil, fmt.Errorf("build plugin tool %q: %w", ref, err)
			}
			tools = append(tools, t)
			// A tools[].ui entry (spec-20 §4.2 method A) also registers as
			// a renderer, alongside — not instead of — being a callable
			// tool: the model decides to call it, the result gets rendered.
			if renderers != nil {
				if reg, ok := RendererRegistrationFromToolSpec(spec, t.Name()); ok {
					*renderers = append(*renderers, reg)
				}
			}
			continue
		}
		t, err := BuildTool(spec, kb, skills)
		if err != nil {
			return nil, nil, fmt.Errorf("build tool %q: %w", ref, err)
		}
		tools = append(tools, t)
	}
	return tools, toolsets, nil
}

// compileHooks resolves capabilities.hooks{} (spec-20 §4.4) — each of the
// five known fields is a list of "plugin:{id}/{point}" refs, authorized
// through the same Authorizer as tools/skills. A ref that isn't a plugin
// hook (or fails to authorize) is silently skipped, same convention as
// compileTools; but two different plugins claiming the same point is a
// hard compile error — unlike renderers, there is no arbitration rule for
// hooks to fall back on.
func compileHooks(ctx context.Context, def map[string]any, authorizer ResourceAuthorizer, out *[]HookRegistration) error {
	caps, _ := def["capabilities"].(map[string]any)
	hooks, _ := caps["hooks"].(map[string]any)
	if hooks == nil || authorizer == nil {
		return nil
	}

	claimed := map[string]string{} // point -> owning plugin ID, for the duplicate-claim check
	for _, point := range HookPoints {
		for _, ref := range toStringList(hooks[point]) {
			spec, ok, err := authorizer.Authorize(ctx, ref)
			if err != nil {
				return fmt.Errorf("authorize hook %q: %w", ref, err)
			}
			if !ok || spec.Kind != KindPluginHook {
				continue
			}
			reg, ok := HookRegistrationFromSpec(spec)
			if !ok {
				continue
			}
			if reg.Point != point {
				return fmt.Errorf("hook %q declares point %q, not %q", ref, reg.Point, point)
			}
			if owner, exists := claimed[point]; exists && owner != reg.PluginID {
				return fmt.Errorf("hook point %q claimed by both %q and %q", point, owner, reg.PluginID)
			}
			claimed[point] = reg.PluginID
			if out != nil {
				*out = append(*out, reg)
			}
		}
	}
	return nil
}

func toStringList(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
