package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

// RouterConfig collects NewRouter's dependencies. AllowedOrigins may be nil
// for local dev (reflects any Origin). IdempotencyStore may be nil to
// disable the Idempotency-Key middleware entirely (e.g. in tests that don't
// need it). Tokens and APIKeys back AuthMiddleware on every route except
// /auth/register and /auth/login (security: [] in the API contract).
type RouterConfig struct {
	AllowedOrigins   []string
	IdempotencyStore IdempotencyStore
	Users            AuthUserStore
	Tokens           *auth.TokenIssuer
	APIKeys          APIKeyLookup
	Resources        *ResourceHandlers
}

// NewRouter assembles the top-level chi router with the shared middleware
// chain — recovery → request_id → logging → cors → auth → rate_limit →
// idempotency — and the /health, /ready probes. /auth/register and
// /auth/login are the only /api/v1 routes exempt from auth. Feature routes
// (Resources, Agents, Bundles, Runs, ...) are mounted under the protected
// group by their respective spec tasks.
func NewRouter(logger *slog.Logger, cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	generalLimiter := NewRateLimiter(600, time.Minute, generalRateLimitKey)
	authHandlers := NewAuthHandlers(cfg.Users, cfg.Tokens)

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
		r.Post("/auth/register", authHandlers.Register)
		r.Post("/auth/login", authHandlers.Login)

		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(cfg.Tokens, cfg.APIKeys))
			r.Use(generalLimiter.Middleware)
			if cfg.IdempotencyStore != nil {
				r.Use(IdempotencyMiddleware(cfg.IdempotencyStore))
			}
			if cfg.Resources != nil {
				r.Get("/resources", cfg.Resources.List)
				r.Post("/resources", cfg.Resources.Create)
				r.Patch("/resources/{id}", cfg.Resources.Update)
				r.Get("/resources/{id}/delete-check", cfg.Resources.DeleteCheck)
			}
			// Further feature routers (Agents, Bundles, Runs, ...) are
			// mounted here by their respective spec tasks. Endpoints that
			// need a tighter limit (e.g. run creation: 20/min) apply an
			// additional NewRateLimiter(20, time.Minute, ...) at the route
			// group level.
		})
	})

	return r
}

// generalRateLimitKey keys the 600/min general limiter by authenticated
// user, falling back to remote address on the rare request that reaches it
// without one.
func generalRateLimitKey(r *http.Request) string {
	if userID, ok := UserIDFromContext(r.Context()); ok {
		return "user:" + strconv.FormatInt(userID, 10)
	}
	return "ip:" + RemoteAddrKey(r)
}
