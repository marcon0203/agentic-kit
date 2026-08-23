// Package api implements the HTTP handlers, middleware and response
// envelope shared by all endpoints of the AI Agent platform.
package api

import (
	"encoding/json"
	"net/http"
)

// Envelope is the unified response wrapper used by every JSON endpoint
// except the NDJSON event stream and a bare 204. See api/openapi.yaml and
// docs/架构设计文档_AI-Agent平台_V1.md 6.2 for the contract. Data is `null`
// when there is no payload — callers must not substitute `{}`, per the
// same spec section.
type Envelope struct {
	Code      int          `json:"code"`
	Message   string       `json:"message"`
	Data      any          `json:"data"`
	RequestID string       `json:"request_id"`
	Details   []FieldError `json:"details,omitempty"`
}

// FieldError describes a single field-level validation failure, used in
// the `details` payload of schema-validation error responses (40001/40002).
// The wire field is `reason`, not `message` — api/openapi.yaml's Envelope
// schema requires [field, reason] on every details entry.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Page wraps a cursor-paginated list per 架构设计文档 6.4. Items must never
// be nil — NewPage guarantees this so list endpoints can never regress into
// serializing `null` for an empty result (the POC's real `.map()` crash).
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// NewPage builds a Page, normalizing a nil items slice to an empty one.
func NewPage[T any](items []T, nextCursor *string, hasMore bool) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, NextCursor: nextCursor, HasMore: hasMore}
}

// writeJSON writes a successful envelope (code 0) with the given data.
// Pass nil when the endpoint has no payload to return.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeEnvelope(w, r, status, 0, "ok", data, nil)
}

// writeErr writes a business-error envelope. code is the five-digit
// business error code (see errors.go); status is the HTTP status.
func writeErr(w http.ResponseWriter, r *http.Request, status, code int, message string) {
	writeEnvelope(w, r, status, code, message, nil, nil)
}

// writeErrDetails writes a business-error envelope with field-level
// validation errors, per 架构设计文档 6.2's `details` example.
func writeErrDetails(w http.ResponseWriter, r *http.Request, status, code int, message string, details []FieldError) {
	writeEnvelope(w, r, status, code, message, nil, details)
}

func writeEnvelope(w http.ResponseWriter, r *http.Request, status, code int, message string, data any, details []FieldError) {
	env := Envelope{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: RequestIDFromContext(r.Context()),
		Details:   details,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}
