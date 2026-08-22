package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewPage_NilItemsBecomesEmptyArray guards the regression this project
// hit for real during the POC: `var out []T` serializes to JSON `null`,
// which crashes a frontend `list.map()`. Every list endpoint must go
// through NewPage so Items can never be nil on the wire.
func TestNewPage_NilItemsBecomesEmptyArray(t *testing.T) {
	var nilItems []string
	page := NewPage(nilItems, nil, false)

	body, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"items":null`) {
		t.Fatalf("items serialized as null, want []: %s", body)
	}
	if !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("expected empty items array, got: %s", body)
	}
}

func TestWriteJSON_EmptyPageInEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)

	page := NewPage[string](nil, nil, false)
	writeJSON(w, r, http.StatusOK, page)

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if strings.Contains(w.Body.String(), `"items":null`) {
		t.Fatalf("envelope leaked null items: %s", w.Body.String())
	}
}

func TestWriteErrDetails_IncludesFieldErrors(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents", nil)

	writeErrDetails(w, r, http.StatusBadRequest, ErrAgentSchemaInvalid, "invalid agent definition",
		[]FieldError{{Field: "capabilities.tools[2]", Message: "资源 mcp/internal-search 已被禁用"}})

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Code != ErrAgentSchemaInvalid {
		t.Fatalf("code = %d, want %d", env.Code, ErrAgentSchemaInvalid)
	}
	if len(env.Details) != 1 || env.Details[0].Field != "capabilities.tools[2]" {
		t.Fatalf("unexpected details: %+v", env.Details)
	}
	if env.Data != nil {
		t.Fatalf("data should be null on error, got %v", env.Data)
	}
}

func TestWriteJSON_DataNullWhenNoPayload(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runs/1/gates/1", nil)

	writeJSON(w, r, http.StatusNoContent, nil)

	if strings.Contains(w.Body.String(), `"data":{}`) {
		t.Fatalf("data must be null, not {}, when there is no payload: %s", w.Body.String())
	}
}
