package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter assembles the top-level chi router with the shared middleware
// stack and the /health, /ready probes. Feature routes are mounted by later
// tasks (auth, resources, agents, bundles, runs, ...).
func NewRouter(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware(logger))
	r.Use(RecoverMiddleware(logger))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Feature routers (Auth, Resources, Agents, Bundles, Runs, ...)
		// are mounted here by their respective spec tasks.
	})

	return r
}
