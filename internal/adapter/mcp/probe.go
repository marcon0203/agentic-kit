// Package mcp adapts an MCP-server reachability probe to the HealthProbe
// port the resource context declares.
package mcp

import (
	"context"
	"net/http"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// probeTimeout bounds the connectivity check spec-05 asks for at
// registration time — long enough for a real server, short enough not to
// hang the create request on a dead endpoint.
const probeTimeout = 5 * time.Second

// ReachabilityProbe treats config["endpoint"] as a URL and calls the server
// healthy if it answers with a non-5xx status.
//
// A real implementation (spec-06/09) would speak the MCP handshake; until
// then "healthy" here means "something answered", not "speaks MCP
// correctly". The port hides that distinction from the domain, which only
// records the verdict.
type ReachabilityProbe struct {
	client *http.Client
}

func NewReachabilityProbe() *ReachabilityProbe {
	return &ReachabilityProbe{client: &http.Client{Timeout: probeTimeout}}
}

var _ resource.HealthProbe = (*ReachabilityProbe)(nil)

func (p *ReachabilityProbe) Check(ctx context.Context, config resource.Config) resource.Health {
	endpoint, ok := config["endpoint"].(string)
	if !ok || endpoint == "" {
		return resource.HealthUnknown
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return resource.HealthUnhealthy
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return resource.HealthUnhealthy
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return resource.HealthUnhealthy
	}
	return resource.HealthHealthy
}
