package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// RouterConfig collects NewRouter's dependencies. AllowedOrigins may be nil
// for local dev (reflects any Origin). IdempotencyStore may be nil to
// disable the Idempotency-Key middleware entirely (e.g. in tests that don't
// need it).
type RouterConfig struct {
	AllowedOrigins   []string
	IdempotencyStore IdempotencyStore
}

// NewRouter assembles the top-level chi router with the shared middleware
// chain — recovery → request_id → logging → cors → rate_limit →
// idempotency — and the /health, /ready probes. Auth (spec-04) inserts
// itself between cors and rate_limit once user context exists; feature
// routes are mounted under /api/v1 by their respective spec tasks.
func NewRouter(logger *slog.Logger, cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	generalLimiter := NewRateLimiter(600, time.Minute, generalRateLimitKey)

	r.Use(RecoverMiddleware(logger))
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware(logger))
	r.Use(CORSMiddleware(cfg.AllowedOrigins))

	// Every response uses the unified envelope, 204 excepted — chi's
	// default 404/405 handlers write a bare "page not found" text body,
	// so they're overridden here to keep that guarantee.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, r, http.StatusMethodNotAllowed, ErrValidationFailed, "method not allowed")
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(generalLimiter.Middleware)
		if cfg.IdempotencyStore != nil {
			r.Use(IdempotencyMiddleware(cfg.IdempotencyStore))
		}
		// Feature routers (Auth, Resources, Agents, Bundles, Runs, ...)
		// are mounted here by their respective spec tasks. Endpoints that
		// need a tighter limit (e.g. run creation: 20/min) apply an
		// additional NewRateLimiter(20, time.Minute, ...) at the route
		// group level.
	})

	return r
}

// generalRateLimitKey keys the 600/min general limiter by authenticated
// user once auth is wired (spec-04), falling back to remote address before
// then so the limiter is meaningful even pre-auth.
func generalRateLimitKey(r *http.Request) string {
	if userID, ok := UserIDFromContext(r.Context()); ok {
		return "user:" + strconv.FormatInt(userID, 10)
	}
	return "ip:" + RemoteAddrKey(r)
}
