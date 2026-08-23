package api

import (
	"log/slog"
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// httpStatusFor is the *only* place a business error kind becomes an HTTP
// status. Domain services never name a status; keeping the mapping in one
// table is what lets the same service sit behind a different transport, and
// stops two handlers from disagreeing about whether "already subscribed" is
// a 409 or a 400.
func httpStatusFor(kind domain.Kind) int {
	switch kind {
	case domain.KindInvalid:
		return http.StatusBadRequest
	case domain.KindUnauthorized:
		return http.StatusUnauthorized
	case domain.KindForbidden:
		return http.StatusForbidden
	case domain.KindNotFound:
		return http.StatusNotFound
	case domain.KindConflict:
		return http.StatusConflict
	case domain.KindUnprocessable:
		return http.StatusUnprocessableEntity
	case domain.KindRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// writeDomainErr renders any error returned by an application service.
// An error that is not a *domain.Error is an unclassified fault: it is
// logged with its cause and reported as a generic 500, so a leaked
// repository or driver error can never reach a client as detail.
func writeDomainErr(w http.ResponseWriter, r *http.Request, err error) {
	derr, classified := domain.AsError(err)
	if !classified || derr.Kind == domain.KindInternal {
		slog.ErrorContext(r.Context(), "unhandled_domain_error",
			"request_id", RequestIDFromContext(r.Context()),
			"path", r.URL.Path,
			"error", err,
		)
	}
	writeEnvelope(w, r, httpStatusFor(derr.Kind), derr.Code, derr.Message, nil, toWireFieldErrors(derr.Details))
}

func toWireFieldErrors(in []domain.FieldError) []FieldError {
	if len(in) == 0 {
		return nil
	}
	out := make([]FieldError, len(in))
	for i, e := range in {
		out[i] = FieldError{Field: e.Field, Reason: e.Reason}
	}
	return out
}

// writeDomainPage renders a domain.Page, re-encoding the raw keyset value as
// an opaque base64 cursor. Cursor opacity is a transport promise, so the
// encoding lives here rather than in a service.
func writeDomainPage[T any](w http.ResponseWriter, r *http.Request, page domain.Page[T]) {
	var next *string
	if page.NextCursor != "" {
		c := encodeCursorString(page.NextCursor)
		next = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(page.Items, next, page.HasMore))
}
