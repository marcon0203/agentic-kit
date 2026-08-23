package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/marcon0203/agentic-kit/internal/observability"
)

type ctxKey string

const (
	requestIDKey ctxKey = "request_id"
	userIDKey    ctxKey = "user_id"
)

// RequestIDFromContext returns the request ID stashed by RequestIDMiddleware,
// falling back to the empty string if none is present (e.g. in tests).
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithUserID attaches the authenticated user's ID to the context. Called by
// the JWT/ApiKey auth middleware (spec-04); consumed here by the
// Idempotency-Key middleware and by rate-limit keying.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext returns the authenticated user's ID, if any.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

// RequestIDMiddleware assigns a request ID and attaches it to the request
// context so handlers and loggers can read it. When an OTel span is active
// (spec-19: the server is wrapped in otelhttp upstream of this middleware),
// the request ID *is* the span's trace ID — "request_id 与 trace_id
// 统一" — so a support engineer can go from an error envelope's
// request_id straight to its trace with no lookup table. Falls back to
// chi's counter-based generator when there's no span (tracing disabled, or
// a handler invoked directly in a unit test).
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := observability.TraceIDFromContext(r.Context())
		if id == "" {
			id = middleware.GetReqID(r.Context())
		}
		if id == "" {
			id = strconv.FormatUint(middleware.NextRequestID(), 10)
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware emits one structured log line per request via slog.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http_request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// RecoverMiddleware converts a panic in a downstream handler into a 500
// envelope response without leaking the stack trace to the client.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic_recovered",
						"request_id", RequestIDFromContext(r.Context()),
						"error", rec,
					)
					writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
