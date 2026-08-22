// Package api implements the HTTP handlers, middleware and response
// envelope shared by all endpoints of the AI Agent platform.
package api

import (
	"encoding/json"
	"net/http"
)

// Envelope is the unified response wrapper used by every JSON endpoint
// except the NDJSON event stream. See api/openapi.yaml for the contract.
type Envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// FieldError describes a single field-level validation failure, used in
// the `details` payload of schema-validation error responses (40001/40002).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// writeJSON writes a successful envelope (code 0) with the given data.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeEnvelope(w, r, status, 0, "ok", data)
}

// writeErr writes a business-error envelope. code is the five-digit
// business error code (see errors.go); status is the HTTP status.
func writeErr(w http.ResponseWriter, r *http.Request, status, code int, message string, data any) {
	writeEnvelope(w, r, status, code, message, data)
}

func writeEnvelope(w http.ResponseWriter, r *http.Request, status, code int, message string, data any) {
	if data == nil {
		data = struct{}{}
	}
	env := Envelope{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: RequestIDFromContext(r.Context()),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}
