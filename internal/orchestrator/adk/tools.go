package adk

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ResourceKind mirrors the four resource-center tables (spec-05): "tool"
// and "mcp" are callable, "skill" contributes instructions rather than a
// callable action.
type ResourceKind string

const (
	KindTool  ResourceKind = "tool"
	KindMCP   ResourceKind = "mcp"
	KindSkill ResourceKind = "skill"
)

// ToolSpec is what an authorized capabilities.tools/skills ref resolves
// to — everything BuildTool needs to construct the ADK tool.Tool, already
// scoped to the requesting owner (the caller did that check).
type ToolSpec struct {
	Ref    string
	Kind   ResourceKind
	Config map[string]any // resource's decrypted config (endpoint, description, ...)
}

// ResourceAuthorizer authorizes one capabilities.tools/skills ref against
// the Resource Registry (spec-05/spec-10 §2: "未授权的资源不进入图 — 不是
// 运行时拦截，而是编译期就不构造这个 Tool"). Implementations live outside
// this package (internal/api, backed by store.Querier) so the compiler
// itself stays free of a database dependency and stays unit-testable with
// a fake. Authorize returns ok=false — not an error — for "this ref simply
// isn't authorized"; a real lookup failure is a genuine error.
type ResourceAuthorizer interface {
	Authorize(ctx context.Context, ref string) (spec ToolSpec, ok bool, err error)
}

const toolCallTimeout = 30 * time.Second

// BuildTool constructs the ADK tool.Tool for one authorized resource. A
// "tool"/"mcp" resource makes a real HTTP call to its configured endpoint
// (mirroring internal/api's MCP connectivity probe — the same "config
// carries an `endpoint`" convention, spec-05); a "skill" has no callable
// action, so it surfaces its own instructions as the tool's result,
// letting the agent read and follow them.
func BuildTool(spec ToolSpec) (tool.Tool, error) {
	switch spec.Kind {
	case KindSkill:
		return buildSkillTool(spec)
	default:
		return buildEndpointTool(spec)
	}
}

type toolArgs struct {
	Input string `json:"input" jsonschema_description:"Input passed to the tool, as free-form text or JSON."`
}
type toolResult struct {
	Output string `json:"output"`
}

func buildEndpointTool(spec ToolSpec) (tool.Tool, error) {
	endpoint, _ := spec.Config["endpoint"].(string)
	description, _ := spec.Config["description"].(string)
	if description == "" {
		description = fmt.Sprintf("Calls the %s resource.", spec.Ref)
	}
	client := &http.Client{Timeout: toolCallTimeout}

	return functiontool.New(functiontool.Config{Name: spec.Ref, Description: description},
		func(ctx agent.ToolContext, args toolArgs) (toolResult, error) {
			output, err := callEndpoint(ctx, client, endpoint, spec.Ref, args.Input)
			return toolResult{Output: output}, err
		})
}

// callEndpoint is buildEndpointTool's HTTP call, factored out so it's
// testable against an httptest.Server without needing a live
// agent.ToolContext (which only ADK's own runtime can construct).
func callEndpoint(ctx context.Context, client *http.Client, endpoint, ref, input string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("resource %q has no endpoint configured", ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(input)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling %s: %w", ref, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: http %d: %s", ref, resp.StatusCode, string(body))
	}
	return string(body), nil
}

func buildSkillTool(spec ToolSpec) (tool.Tool, error) {
	instructions := skillInstructions(spec)
	description := fmt.Sprintf("Retrieves the %q skill's instructions to follow.", spec.Ref)

	return functiontool.New(functiontool.Config{Name: spec.Ref, Description: description},
		func(ctx agent.ToolContext, args toolArgs) (toolResult, error) {
			return toolResult{Output: instructions}, nil
		})
}

func skillInstructions(spec ToolSpec) string {
	if s, _ := spec.Config["instructions"].(string); s != "" {
		return s
	}
	s, _ := spec.Config["description"].(string)
	return s
}
