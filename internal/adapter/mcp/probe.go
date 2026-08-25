// Package mcp adapts a real MCP-protocol probe to the HealthProbe port the
// resource context declares, and exposes the same probe as a standalone
// function for the preview endpoint (POST /resources/mcp/probe).
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// probeTimeout bounds the connectivity check spec-05 asks for at
// registration time — long enough for a real server, short enough not to
// hang the create request on a dead endpoint.
const probeTimeout = 8 * time.Second

// ToolInfo is one tool an MCP server advertised via tools/list.
type ToolInfo struct {
	Name        string
	Description string
}

// Probe speaks the real MCP handshake against endpoint (initialize, then
// tools/list) using the raw MCP SDK client directly — not ADK's
// mcptoolset, which needs an agent.ReadonlyContext that only exists inside
// a real run. headers are sent on every request the client makes, letting
// a caller pass an Authorization header or any custom header the server
// requires.
func Probe(ctx context.Context, endpoint string, headers map[string]string) ([]ToolInfo, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("no endpoint configured")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	client := &http.Client{Timeout: probeTimeout}
	if len(headers) > 0 {
		client.Transport = &headerTransport{headers: headers, base: http.DefaultTransport}
	}

	mcpClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "agentic-kit", Version: "1"}, nil)
	session, err := mcpClient.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	tools := make([]ToolInfo, 0, len(result.Tools))
	for _, t := range result.Tools {
		tools = append(tools, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return tools, nil
}

// headerTransport adds a fixed set of headers to every request — the MCP
// SDK's client takes an *http.Client, not a per-request header option, so
// this is how a probe's or a registered resource's configured headers
// actually reach the server.
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

// Prober implements resource.ToolProbe — the preview-only "what tools does
// this endpoint have" check a registration page runs before saving
// anything, as opposed to ReachabilityProbe below (which runs at Create
// time and gets persisted as Resource.Health).
type Prober struct{}

func NewProber() *Prober { return &Prober{} }

var _ resource.ToolProbe = (*Prober)(nil)

func (p *Prober) Probe(ctx context.Context, endpoint string, headers map[string]string) ([]resource.ProbedTool, error) {
	tools, err := Probe(ctx, endpoint, headers)
	if err != nil {
		return nil, err
	}
	out := make([]resource.ProbedTool, len(tools))
	for i, t := range tools {
		out[i] = resource.ProbedTool{Name: t.Name, Description: t.Description}
	}
	return out, nil
}

// ReachabilityProbe implements resource.HealthProbe by speaking a real MCP
// handshake — "healthy" means the server actually completed initialize and
// answered tools/list, not "a GET returned a non-5xx status" (this
// package's original implementation, which never spoke MCP at all).
type ReachabilityProbe struct{}

func NewReachabilityProbe() *ReachabilityProbe { return &ReachabilityProbe{} }

var _ resource.HealthProbe = (*ReachabilityProbe)(nil)

func (p *ReachabilityProbe) Check(ctx context.Context, config resource.Config) resource.Health {
	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		return resource.HealthUnknown
	}
	if _, err := Probe(ctx, endpoint, HeadersFromConfig(config)); err != nil {
		return resource.HealthUnhealthy
	}
	return resource.HealthHealthy
}

// HeadersFromConfig reads an MCP resource's config.headers ([]any of
// {"key","value"} objects) plus the legacy single api_key field (sent as a
// bearer token, matching how this platform has always authenticated MCP
// calls) into one header map. config's values must already be plaintext —
// callers pass either a not-yet-encrypted CreateCommand.Config or an
// already-decrypted one, never ciphertext.
func HeadersFromConfig(config resource.Config) map[string]string {
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
