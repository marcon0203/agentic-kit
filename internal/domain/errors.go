// Package domain is the shared kernel of the platform's bounded contexts:
// the business error model and pagination types every context speaks.
//
// Layering (hexagonal / DDD):
//
//	internal/domain/<context>   entities, application services, repository
//	                            PORTS (interfaces the context declares for
//	                            what it needs) and domain errors
//	internal/adapter/postgres   repository ADAPTERS — the sqlc-backed
//	                            implementations of those ports
//	internal/api                transport only: decode a request, call a
//	                            service, translate a domain error into an
//	                            HTTP status + envelope, encode a response
//
// Dependencies point inward: adapters and transport import domain, never
// the reverse. A domain package must not import net/http, chi, pgx, pgtype
// or internal/store — if it does, business logic has leaked back out into
// the infrastructure.
package domain

import (
	"errors"
	"fmt"
)

// Kind classifies a domain error by *what went wrong in business terms*,
// deliberately not by HTTP status — internal/api owns that translation, so
// the same service can sit behind a different transport without dragging
// HTTP semantics into the domain.
type Kind int

const (
	KindInternal Kind = iota
	KindInvalid
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
	KindUnprocessable
	KindRateLimited
)

// FieldError describes a single field-level problem. It is the domain's own
// type: internal/api converts it to the wire shape, so changing the JSON
// contract never reaches in here.
type FieldError struct {
	Field  string
	Reason string
}

// Error is the single error type every application service returns. It
// carries the five-digit business code because those codes are part of the
// platform's ubiquitous language, not a transport detail — docs/架构设计文档
// 6.3 defines them as the business error table, and the frontend switches on
// them directly. HTTP status is derived from Kind at the edge instead.
type Error struct {
	Kind    Kind
	Code    int
	Message string
	Details []FieldError
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s (code=%d): %v", e.Message, e.Code, e.cause)
	}
	return fmt.Sprintf("%s (code=%d)", e.Message, e.Code)
}

func (e *Error) Unwrap() error { return e.cause }

// WithDetails returns a copy carrying field-level details.
func (e *Error) WithDetails(details ...FieldError) *Error {
	clone := *e
	clone.Details = details
	return &clone
}

// WithCause returns a copy wrapping the underlying cause. The cause is for
// logs and errors.Is/As — it is never surfaced to a client.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

func newError(kind Kind, code int, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Constructors — one per Kind, so a service reads as business intent
// ("this is a conflict") rather than as a status-code choice.
func Invalid(code int, message string) *Error     { return newError(KindInvalid, code, message) }
func Unauthorized(code int, msg string) *Error    { return newError(KindUnauthorized, code, msg) }
func Forbidden(code int, message string) *Error   { return newError(KindForbidden, code, message) }
func NotFound(code int, message string) *Error    { return newError(KindNotFound, code, message) }
func Conflict(code int, message string) *Error    { return newError(KindConflict, code, message) }
func Unprocessable(code int, msg string) *Error   { return newError(KindUnprocessable, code, msg) }
func RateLimited(code int, message string) *Error { return newError(KindRateLimited, code, message) }

// Internal wraps an infrastructure failure. The message is deliberately
// fixed and generic: internal faults must never leak driver or query text
// to a client, and every call site would otherwise reinvent the wording.
func Internal(cause error) *Error {
	return &Error{Kind: KindInternal, Code: CodeInternal, Message: "internal server error", cause: cause}
}

// AsError extracts a *Error from err, reporting whether one was found.
// Anything that is not a domain error is an unclassified fault and is
// reported as internal — that default is what stops an un-wrapped
// repository error from being rendered as a 200 or a stray 400.
func AsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var de *Error
	if errors.As(err, &de) {
		return de, true
	}
	return Internal(err), false
}
