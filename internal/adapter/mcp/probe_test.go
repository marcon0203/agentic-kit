package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	adaptermcp "github.com/marcon0203/agentic-kit/internal/adapter/mcp"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// newTestMCPServer spins up a real in-process MCP server (streamable HTTP)
// advertising one tool, and records the Authorization header (or any
// header) seen on each request — this exercises the real MCP handshake,
// not a stubbed HTTP endpoint, per this platform's "no bare GET" fix.
func newTestMCPServer(t *testing.T, requiredHeader, requiredValue string) (url string, seenHeaders func() http.Header) {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test-server", Version: "v1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo", Description: "Echoes its input."},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, _ any) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{}, nil, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)

	var last http.Header
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r.Header.Clone()
		if requiredHeader != "" && r.Header.Get(requiredHeader) != requiredValue {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	return httpServer.URL, func() http.Header { return last }
}

func TestProbe_ListsRealToolsOverMCPHandshake(t *testing.T) {
	url, _ := newTestMCPServer(t, "", "")

	tools, err := adaptermcp.Probe(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("expected the server's one real tool, got %+v", tools)
	}
}

func TestProbe_SendsConfiguredHeaders(t *testing.T) {
	url, seen := newTestMCPServer(t, "Authorization", "Bearer sk-test")

	if _, err := adaptermcp.Probe(context.Background(), url, map[string]string{"Authorization": "Bearer sk-test"}); err != nil {
		t.Fatalf("Probe with the required header should succeed: %v", err)
	}
	if seen().Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("server should have received the configured header, got %q", seen().Get("Authorization"))
	}
}

func TestProbe_WrongHeader_Fails(t *testing.T) {
	url, _ := newTestMCPServer(t, "Authorization", "Bearer sk-test")

	if _, err := adaptermcp.Probe(context.Background(), url, nil); err == nil {
		t.Fatal("expected an error when the required header is missing")
	}
}

func TestProbe_NoEndpoint_ReturnsError(t *testing.T) {
	if _, err := adaptermcp.Probe(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error for an empty endpoint")
	}
}

func TestReachabilityProbe_SpeaksRealMCPNotBareHTTP(t *testing.T) {
	url, _ := newTestMCPServer(t, "", "")
	probe := adaptermcp.NewReachabilityProbe()

	got := probe.Check(context.Background(), resource.Config{"endpoint": url})
	if got != resource.HealthHealthy {
		t.Fatalf("expected healthy against a real MCP server, got %v", got)
	}
}

func TestReachabilityProbe_NonMCPServer_Unhealthy(t *testing.T) {
	// A server that answers plain HTTP (not MCP) must be judged unhealthy —
	// the old implementation would have called this healthy on any non-5xx
	// status, which was the entire bug this rewrite fixes.
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello, this is not MCP"))
	}))
	defer httpServer.Close()
	probe := adaptermcp.NewReachabilityProbe()

	got := probe.Check(context.Background(), resource.Config{"endpoint": httpServer.URL})
	if got != resource.HealthUnhealthy {
		t.Fatalf("expected unhealthy against a non-MCP server, got %v", got)
	}
}

func TestHeadersFromConfig_MergesLegacyAPIKeyAndHeaderList(t *testing.T) {
	headers := adaptermcp.HeadersFromConfig(resource.Config{
		"api_key": "legacy-key",
		"headers": []any{map[string]any{"key": "X-Custom", "value": "custom-value"}},
	})
	if headers["Authorization"] != "Bearer legacy-key" {
		t.Fatalf("expected legacy api_key to become a bearer Authorization header, got %q", headers["Authorization"])
	}
	if headers["X-Custom"] != "custom-value" {
		t.Fatalf("expected the custom header to pass through, got %q", headers["X-Custom"])
	}
}
