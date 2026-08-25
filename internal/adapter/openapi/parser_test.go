package openapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const petstoreSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Petstore", "version": "1.0"},
  "servers": [{"url": "https://api.example.com/v1"}],
  "paths": {
    "/pets": {
      "get": {"operationId": "listPets", "summary": "List pets", "responses": {"200": {"description": "ok"}}},
      "post": {"summary": "Create a pet", "responses": {"200": {"description": "ok"}}}
    },
    "/pets/{id}": {
      "get": {"operationId": "get Pet!", "summary": "Get a pet", "responses": {"200": {"description": "ok"}}}
    }
  }
}`

func TestParse_FromContent_ReturnsOperationsInStableOrder(t *testing.T) {
	p := NewParser()
	result, err := p.Parse(context.Background(), "", petstoreSpec)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected base URL: %q", result.BaseURL)
	}
	if len(result.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d: %+v", len(result.Operations), result.Operations)
	}
	// /pets sorts before /pets/{id}; within /pets, GET sorts before POST.
	if result.Operations[0].OperationID != "listpets" || result.Operations[0].Path != "/pets" || result.Operations[0].Method != "GET" {
		t.Fatalf("unexpected first operation: %+v", result.Operations[0])
	}
	if result.Operations[1].Method != "POST" || result.Operations[1].OperationID == "" {
		t.Fatalf("unexpected second operation: %+v", result.Operations[1])
	}
}

func TestParse_SanitizesOperationIDToRefSafeSegment(t *testing.T) {
	p := NewParser()
	result, err := p.Parse(context.Background(), "", petstoreSpec)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, op := range result.Operations {
		if op.OperationID == "" {
			t.Fatalf("expected a non-empty operation id: %+v", op)
		}
		for _, r := range op.OperationID {
			isLower := r >= 'a' && r <= 'z'
			isDigit := r >= '0' && r <= '9'
			if !isLower && !isDigit && r != '_' {
				t.Fatalf("operation id %q contains an unsafe character %q", op.OperationID, r)
			}
		}
		if op.OperationID[0] < 'a' || op.OperationID[0] > 'z' {
			t.Fatalf("operation id %q does not start with a lowercase letter", op.OperationID)
		}
	}
}

func TestParse_FromURL_FetchesAndParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(petstoreSpec))
	}))
	defer srv.Close()

	p := NewParser()
	result, err := p.Parse(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(result.Operations))
	}
}

func TestParse_EmptySpec_ReturnsError(t *testing.T) {
	p := NewParser()
	if _, err := p.Parse(context.Background(), "", ""); err == nil {
		t.Fatal("expected an error for an empty spec")
	}
}

func TestParse_InvalidJSON_ReturnsError(t *testing.T) {
	p := NewParser()
	if _, err := p.Parse(context.Background(), "", "not a spec"); err == nil {
		t.Fatal("expected an error for an unparseable spec")
	}
}

func TestParse_HTTPFailure_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewParser()
	_, err := p.Parse(context.Background(), srv.URL, "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a 404 error, got %v", err)
	}
}
