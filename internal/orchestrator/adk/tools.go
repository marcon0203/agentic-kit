package adk

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/exitlooptool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/adk/tool/loadartifactstool"
	"google.golang.org/adk/tool/loadmemorytool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/adk/tool/preloadmemorytool"
)

// BuiltinToolNames are the ADK-shipped tools an Agent's
// capabilities.builtin_tools[] may name (schemas/agent.schema.json's enum
// mirrors this list exactly). Unlike tool/skill/mcp/knowledge_base, these
// aren't registered in the resource center at all — they're the SDK's own
// implementations, wired in directly at compile time.
const (
	BuiltinGoogleSearch  = "google_search"
	BuiltinLoadMemory    = "load_memory"
	BuiltinPreloadMemory = "preload_memory"
	BuiltinLoadArtifacts = "load_artifacts"
	BuiltinExitLoop      = "exit_loop"
)

// BuildBuiltinTool maps one capabilities.builtin_tools[] entry to the ADK
// SDK's own tool implementation. load_memory/preload_memory only do
// anything useful when the run's agent.Memory is actually wired (see
// ADKRunner.MemoryService) — building them here doesn't require that; not
// having it just means they search an empty memory.
func BuildBuiltinTool(name string) (tool.Tool, error) {
	switch name {
	case BuiltinGoogleSearch:
		return geminitool.GoogleSearch{}, nil
	case BuiltinLoadMemory:
		return loadmemorytool.New(), nil
	case BuiltinPreloadMemory:
		return preloadmemorytool.New(), nil
	case BuiltinLoadArtifacts:
		return loadartifactstool.New(), nil
	case BuiltinExitLoop:
		return exitlooptool.New()
	default:
		return nil, fmt.Errorf("unknown builtin tool %q", name)
	}
}

// ResourceKind mirrors the four resource-center tables (spec-05): "tool"
// and "mcp" are callable, "skill" contributes instructions rather than a
// callable action, "knowledge_base" is a real vector-search call.
type ResourceKind string

const (
	KindTool          ResourceKind = "tool"
	KindMCP           ResourceKind = "mcp"
	KindSkill         ResourceKind = "skill"
	KindKnowledgeBase ResourceKind = "knowledge_base"
)

// ToolSpec is what an authorized capabilities.tools/skills ref resolves
// to — everything BuildTool needs to construct the ADK tool.Tool, already
// scoped to the requesting owner (the caller did that check). OwnerID and
// ResourceID are only used by KindKnowledgeBase, to call the search
// service scoped correctly; the other kinds carry everything they need in
// Config already.
type ToolSpec struct {
	Ref        string
	Kind       ResourceKind
	Config     map[string]any // resource's decrypted config (endpoint, description, ...)
	OwnerID    int64
	ResourceID int64
}

// KnowledgeBaseSearchResult is one ranked chunk returned by a
// KnowledgeBaseSearcher — a package-local mirror of
// internal/domain/knowledgebase.SearchResult so this package doesn't need
// to import the domain layer directly (spec-10: ADK wiring stays
// self-contained; the concrete searcher is injected from outside).
type KnowledgeBaseSearchResult struct {
	SourceRef string
	Content   string
	Score     float64
}

// KnowledgeBaseSearcher answers a knowledge_base resource's real
// vector-search query. Implementations live outside this package
// (internal/adapter/orchestrator, backed by internal/domain/knowledgebase).
type KnowledgeBaseSearcher interface {
	Search(ctx context.Context, ownerID, knowledgeBaseID int64, query string, topK int) ([]KnowledgeBaseSearchResult, error)
}

// SkillContentFetcher fetches a "skill" resource's SKILL.md body from OSS
// at call time — only used when the resource's config.oss_prefix is set
// (spec-05a: zip-uploaded Skills). ossPrefix is the resource's own stored
// prefix (e.g. "skills/{owner}/{ref}/1.0"); implementations live outside
// this package (internal/adapter/orchestrator, backed by
// internal/domain/resource.ObjectStore) so this package stays free of an
// OSS/domain dependency.
type SkillContentFetcher interface {
	Fetch(ctx context.Context, ownerID, resourceID int64, ossPrefix string) (string, error)
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
// "tool" resource makes a real HTTP call to its configured endpoint
// (mirroring internal/api's MCP connectivity probe — the same "config
// carries an `endpoint`" convention, spec-05); a "skill" has no callable
// action, so it surfaces its own instructions as the tool's result,
// letting the agent read and follow them; a "knowledge_base" calls kb's
// real vector search. "mcp" is handled separately by BuildMCPToolset (it
// produces a tool.Toolset, not a single tool.Tool — an MCP server can
// expose any number of tools, discovered at connect time).
func BuildTool(spec ToolSpec, kb KnowledgeBaseSearcher, skills SkillContentFetcher) (tool.Tool, error) {
	switch spec.Kind {
	case KindSkill:
		return buildSkillTool(spec, skills)
	case KindKnowledgeBase:
		return buildKnowledgeBaseTool(spec, kb)
	case KindMCP:
		return nil, fmt.Errorf("resource %q: mcp resources build a tool.Toolset via BuildMCPToolset, not BuildTool", spec.Ref)
	default:
		return buildEndpointTool(spec)
	}
}

// BuildMCPToolset connects to a "mcp" resource's real MCP server (via
// ADK's mcptoolset, a genuine JSON-RPC MCP client) and exposes whatever
// tools that server advertises. Unlike every other resource kind, the
// number and shape of tools aren't known until connect time — that's why
// this returns a tool.Toolset rather than a single tool.Tool.
//
// spec.Config carries the same "endpoint" convention as buildEndpointTool
// (mirroring internal/api's MCP connectivity probe), plus either a
// config.headers list (arbitrary header rows a user typed in at
// registration — see spec-05a) or the older single "api_key" field (sent
// as a Bearer token), on every request to the server. Both can be present
// at once; headers just adds to whatever api_key already set.
func BuildMCPToolset(spec ToolSpec) (tool.Toolset, error) {
	endpoint, _ := spec.Config["endpoint"].(string)
	if endpoint == "" {
		return nil, fmt.Errorf("resource %q has no endpoint configured", spec.Ref)
	}

	client := &http.Client{Timeout: toolCallTimeout}
	if headers := headersFromConfig(spec.Config); len(headers) > 0 {
		client.Transport = &headerTransport{headers: headers, base: http.DefaultTransport}
	}

	return mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client},
	})
}

// headersFromConfig reads an MCP resource's config.headers ([]any of
// {"key","value"} objects) plus the legacy single api_key field into one
// header map — the same shape internal/adapter/mcp.HeadersFromConfig reads
// for the connectivity probe, duplicated here rather than imported to keep
// this package decoupled from the domain layer (spec-10's "所有 ADK 调用
// 收敛在 internal/orchestrator/adk 包内" convention).
func headersFromConfig(config map[string]any) map[string]string {
	headers := map[string]string{}
	if apiKey, _ := config["api_key"].(string); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	raw, _ := config["headers"].([]any)
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		value, _ := m["value"].(string)
		if key != "" && value != "" {
			headers[key] = value
		}
	}
	return headers
}

// headerTransport adds a fixed set of headers to every request — the MCP
// transport takes an *http.Client, not a per-request header option, so
// this is how a resource's stored credentials reach the server.
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

type kbSearchArgs struct {
	Query string `json:"query" jsonschema_description:"The question or topic to search this knowledge base for."`
}
type kbSearchResult struct {
	Results []KnowledgeBaseSearchResult `json:"results"`
}

func buildKnowledgeBaseTool(spec ToolSpec, kb KnowledgeBaseSearcher) (tool.Tool, error) {
	description, _ := spec.Config["description"].(string)
	if description == "" {
		description = fmt.Sprintf("Searches the %q knowledge base and returns the most relevant passages.", spec.Ref)
	}
	if kb == nil {
		return nil, fmt.Errorf("resource %q: no knowledge base searcher configured", spec.Ref)
	}

	return functiontool.New(functiontool.Config{Name: spec.Ref, Description: description},
		func(ctx agent.ToolContext, args kbSearchArgs) (kbSearchResult, error) {
			results, err := kb.Search(ctx, spec.OwnerID, spec.ResourceID, args.Query, 0)
			if err != nil {
				return kbSearchResult{}, err
			}
			return kbSearchResult{Results: results}, nil
		})
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

// buildSkillTool surfaces a Skill's instructions as the tool's result. A
// zip-uploaded Skill (config.oss_prefix set, spec-05a) fetches its SKILL.md
// from OSS at call time via skills; an older/config-only Skill falls back
// to config.instructions the way it always has.
func buildSkillTool(spec ToolSpec, skills SkillContentFetcher) (tool.Tool, error) {
	description := fmt.Sprintf("Retrieves the %q skill's instructions to follow.", spec.Ref)
	ossPrefix, _ := spec.Config["oss_prefix"].(string)

	return functiontool.New(functiontool.Config{Name: spec.Ref, Description: description},
		func(ctx agent.ToolContext, args toolArgs) (toolResult, error) {
			if ossPrefix != "" && skills != nil {
				content, err := skills.Fetch(ctx, spec.OwnerID, spec.ResourceID, ossPrefix)
				if err != nil {
					return toolResult{}, fmt.Errorf("fetch skill %q content: %w", spec.Ref, err)
				}
				return toolResult{Output: content}, nil
			}
			return toolResult{Output: skillInstructions(spec)}, nil
		})
}

func skillInstructions(spec ToolSpec) string {
	if s, _ := spec.Config["instructions"].(string); s != "" {
		return s
	}
	s, _ := spec.Config["description"].(string)
	return s
}
