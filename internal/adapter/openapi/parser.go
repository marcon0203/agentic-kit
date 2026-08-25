// Package openapi implements resource.OpenAPIParser — parsing an OpenAPI
// spec (fetched by URL, or given directly) into the operations 组件's
// OpenAPI import preview step shows (spec-05a §4). Nothing here touches the
// database; that's CreateComponentsBatch's job, one step later.
package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

const fetchTimeout = 15 * time.Second

// Parser implements resource.OpenAPIParser using kin-openapi.
type Parser struct{ client *http.Client }

func NewParser() *Parser { return &Parser{client: &http.Client{Timeout: fetchTimeout}} }

var _ resource.OpenAPIParser = (*Parser)(nil)

// Parse loads a spec — from specURL if set, else specContent directly — and
// flattens every path×method into an OpenAPIOperation, sorted by path then
// method so the preview list is stable across calls.
func (p *Parser) Parse(ctx context.Context, specURL, specContent string) (resource.OpenAPIParseResult, error) {
	data := []byte(specContent)
	if specURL != "" {
		fetched, err := p.fetch(ctx, specURL)
		if err != nil {
			return resource.OpenAPIParseResult{}, fmt.Errorf("fetch spec: %w", err)
		}
		data = fetched
	}
	if len(data) == 0 {
		return resource.OpenAPIParseResult{}, fmt.Errorf("spec is empty")
	}

	doc, err := openapi3.NewLoader().LoadFromData(data)
	if err != nil {
		return resource.OpenAPIParseResult{}, fmt.Errorf("parse spec: %w", err)
	}

	var baseURL string
	if len(doc.Servers) > 0 {
		baseURL = doc.Servers[0].URL
	}

	var operations []resource.OpenAPIOperation
	if doc.Paths != nil {
		byPath := doc.Paths.Map()
		paths := make([]string, 0, len(byPath))
		for path := range byPath {
			paths = append(paths, path)
		}
		sort.Strings(paths)

		seen := map[string]int{}
		for _, path := range paths {
			pathItem := byPath[path]
			byMethod := pathItem.Operations()
			methods := make([]string, 0, len(byMethod))
			for method := range byMethod {
				methods = append(methods, method)
			}
			sort.Strings(methods)

			for _, method := range methods {
				op := byMethod[method]
				id := sanitizeOperationID(op.OperationID, method, path)
				seen[id]++
				if seen[id] > 1 {
					id = fmt.Sprintf("%s_%d", id, seen[id])
				}
				operations = append(operations, resource.OpenAPIOperation{
					OperationID: id,
					Method:      method,
					Path:        path,
					Summary:     op.Summary,
				})
			}
		}
	}

	return resource.OpenAPIParseResult{BaseURL: baseURL, Operations: operations}, nil
}

func (p *Parser) fetch(ctx context.Context, specURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

var refUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// sanitizeOperationID turns a spec's operationId (or, missing one, its
// method+path) into a ref-safe segment — lowercase, only [a-z0-9_],
// starting with a letter — since it becomes the second half of
// `{base_ref}__{operation_id}`, which must itself pass resource's ref
// pattern (`^[a-z][a-z0-9_-]*$`).
func sanitizeOperationID(operationID, method, path string) string {
	src := operationID
	if src == "" {
		src = method + "_" + path
	}
	safe := refUnsafe.ReplaceAllString(strings.ToLower(src), "_")
	safe = strings.Trim(safe, "_")
	if safe == "" || safe[0] < 'a' || safe[0] > 'z' {
		safe = "op_" + safe
	}
	return safe
}
