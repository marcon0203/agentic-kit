package api

import (
	"encoding/json"
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// ImportOpenAPI handles POST /resources/components/import-openapi — parses a
// spec (by URL or pasted content) and returns its operations, without
// persisting anything. The "预览" step of 组件's OpenAPI import (spec-05a
// §4): CreateComponentsBatch is the separate call that actually registers
// the operations a person picks out of this list.
func (h *ResourceHandlers) ImportOpenAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserIDFromContext(r.Context()); !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req importOpenAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	result, err := h.svc.ImportOpenAPIPreview(r.Context(), req.SpecURL, req.SpecContent)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	ops := make([]openAPIOperationDTO, len(result.Operations))
	for i, op := range result.Operations {
		ops[i] = openAPIOperationDTO{OperationID: op.OperationID, Method: op.Method, Path: op.Path, Summary: op.Summary}
	}
	writeJSON(w, r, http.StatusOK, importOpenAPIResponse{BaseURL: result.BaseURL, Operations: ops})
}

// BatchCreateComponents handles POST /resources/components/batch — the
// "勾选 → 批量创建" step: each selected operation becomes its own `tools`
// row (`{base_ref}__{operation_id}`), all in one transaction.
func (h *ResourceHandlers) BatchCreateComponents(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req batchCreateComponentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	ops := make([]resource.OpenAPIOperation, len(req.Operations))
	for i, op := range req.Operations {
		ops[i] = resource.OpenAPIOperation{OperationID: op.OperationID, Method: op.Method, Path: op.Path, Summary: op.Summary}
	}

	created, err := h.svc.CreateComponentsBatch(r.Context(), userID, resource.BatchCreateComponentsCommand{
		BaseRef: req.BaseRef, BaseURL: req.BaseURL, Operations: ops,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	dtos := make([]resourceDTO, len(created))
	for i, res := range created {
		dtos[i] = toResourceDTO(res)
	}
	writeJSON(w, r, http.StatusCreated, map[string]any{"items": dtos})
}

type importOpenAPIRequest struct {
	SpecURL     string `json:"spec_url"`
	SpecContent string `json:"spec_content"`
}

type openAPIOperationDTO struct {
	OperationID string `json:"operation_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Summary     string `json:"summary,omitempty"`
}

type importOpenAPIResponse struct {
	BaseURL    string                `json:"base_url,omitempty"`
	Operations []openAPIOperationDTO `json:"operations"`
}

type batchCreateComponentsRequest struct {
	BaseRef    string                `json:"base_ref"`
	BaseURL    string                `json:"base_url"`
	Operations []openAPIOperationDTO `json:"operations"`
}
