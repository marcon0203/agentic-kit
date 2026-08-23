package api

import (
	"context"
	"net/http"
	"time"
)

// mcpProbeTimeout bounds the connectivity check spec-05 asks for at
// registration time — long enough for a real server, short enough not to
// hang the create request on a dead endpoint.
const mcpProbeTimeout = 5 * time.Second

// MCPHealthChecker probes an MCP server's reachability. A real
// implementation (spec-06/09) would speak the MCP handshake; this HTTP
// reachability check is what registration and the periodic refresh job use
// until then — "healthy" here means "something answered", not "speaks MCP
// correctly".
type MCPHealthChecker interface {
	Check(ctx context.Context, config map[string]any) string // "healthy" | "unhealthy" | "unknown"
}

// httpReachabilityChecker treats config["endpoint"] as a URL and considers
// the server healthy if it answers with a non-5xx status.
type httpReachabilityChecker struct {
	client *http.Client
}

func NewHTTPReachabilityChecker() *httpReachabilityChecker {
	return &httpReachabilityChecker{client: &http.Client{Timeout: mcpProbeTimeout}}
}

func (c *httpReachabilityChecker) Check(ctx context.Context, config map[string]any) string {
	endpoint, ok := config["endpoint"].(string)
	if !ok || endpoint == "" {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(ctx, mcpProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "unhealthy"
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "unhealthy"
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return "unhealthy"
	}
	return "healthy"
}
