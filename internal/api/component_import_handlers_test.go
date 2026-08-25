package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

type fakeOpenAPIParser struct {
	result resource.OpenAPIParseResult
	err    error
}

func (f *fakeOpenAPIParser) Parse(_ context.Context, _, _ string) (resource.OpenAPIParseResult, error) {
	return f.result, f.err
}

func newResourceHandlersWithOpenAPIImport(parser resource.OpenAPIParser) (*ResourceHandlers, *fakeResourceRepo) {
	repo := newFakeResourceRepo()
	svc := resource.NewService(repo, passthroughCipher{}, healthyProbe{}, true).WithOpenAPIImport(parser)
	return NewResourceHandlers(svc, fakeToolProbe{}), repo
}

func TestImportOpenAPI_RequiresAuth(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	body, _ := json.Marshal(importOpenAPIRequest{SpecURL: "https://example.com/spec.json"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resources/components/import-openapi", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ImportOpenAPI(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestImportOpenAPI_NotConfigured_ReturnsError(t *testing.T) {
	h, _ := newResourceHandlersForTest() // no .WithOpenAPIImport
	w := doResourceRequest(t, h.ImportOpenAPI, http.MethodPost, "/api/v1/resources/components/import-openapi", 1,
		importOpenAPIRequest{SpecURL: "https://example.com/spec.json"}, nil)
	if w.Code == http.StatusOK {
		t.Fatalf("expected a non-200 for an unconfigured deployment, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportOpenAPI_ReturnsOperations(t *testing.T) {
	parser := &fakeOpenAPIParser{result: resource.OpenAPIParseResult{
		BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{
			{OperationID: "list_pets", Method: "GET", Path: "/pets", Summary: "List pets"},
		},
	}}
	h, _ := newResourceHandlersWithOpenAPIImport(parser)

	w := doResourceRequest(t, h.ImportOpenAPI, http.MethodPost, "/api/v1/resources/components/import-openapi", 1,
		importOpenAPIRequest{SpecURL: "https://example.com/spec.json"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestBatchCreateComponents_RequiresAuth(t *testing.T) {
	h, _ := newResourceHandlersForTest()
	body, _ := json.Marshal(batchCreateComponentsRequest{})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resources/components/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.BatchCreateComponents(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestBatchCreateComponents_CreatesOneToolPerOperation(t *testing.T) {
	h, repo := newResourceHandlersWithOpenAPIImport(&fakeOpenAPIParser{})

	w := doResourceRequest(t, h.BatchCreateComponents, http.MethodPost, "/api/v1/resources/components/batch", 1,
		batchCreateComponentsRequest{
			BaseRef: "petstore", BaseURL: "https://api.example.com",
			Operations: []openAPIOperationDTO{
				{OperationID: "list_pets", Method: "GET", Path: "/pets", Summary: "List pets"},
				{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}", Summary: "Get a pet"},
			},
		}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if len(repo.rows) != 2 {
		t.Fatalf("expected 2 resources created, got %d", len(repo.rows))
	}
}

func TestBatchCreateComponents_ValidationError(t *testing.T) {
	h, _ := newResourceHandlersWithOpenAPIImport(&fakeOpenAPIParser{})

	w := doResourceRequest(t, h.BatchCreateComponents, http.MethodPost, "/api/v1/resources/components/batch", 1,
		batchCreateComponentsRequest{}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
