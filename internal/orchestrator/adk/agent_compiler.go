package adk

import (
	"context"
	"fmt"

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

	tools, toolsets, err := compileTools(ctx, def, opts.Authorizer, opts.KnowledgeBaseSearcher)
	if err != nil {
		return nil, fmt.Errorf("adk: agent %q: %w", ref, err)
	}

	llm := NewGatewayLLM(opts.Gateway, primary, fallbacks, opts.Credentials)

	a, err := llmagent.New(llmagent.Config{
		Name:        ref,
		Description: role,
		Instruction: persona,
		Model:       llm,
		Tools:       tools,
		Toolsets:    toolsets,
		OutputKey:   ref,
	})
	if err != nil {
		return nil, fmt.Errorf("adk: agent %q: compile llmagent: %w", ref, err)
	}
	return a, nil
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

func compileTools(ctx context.Context, def map[string]any, authorizer ResourceAuthorizer, kb KnowledgeBaseSearcher) ([]tool.Tool, []tool.Toolset, error) {
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
		if componentType, _ := spec.Config["component_type"].(string); componentType == "sandbox" {
			sandboxTools, err := BuildSandboxTools(spec)
			if err != nil {
				return nil, nil, fmt.Errorf("build sandbox tools %q: %w", ref, err)
			}
			tools = append(tools, sandboxTools...)
			continue
		}
		t, err := BuildTool(spec, kb)
		if err != nil {
			return nil, nil, fmt.Errorf("build tool %q: %w", ref, err)
		}
		tools = append(tools, t)
	}
	return tools, toolsets, nil
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
