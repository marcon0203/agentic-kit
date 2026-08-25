package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

// Pinger is the readiness dependency check — satisfied by *pgxpool.Pool.
// Kept as a narrow interface so this package doesn't need to import pgxpool
// just for a health check.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterConfig collects NewRouter's dependencies. AllowedOrigins may be nil
// for local dev (reflects any Origin). IdempotencyStore may be nil to
// disable the Idempotency-Key middleware entirely (e.g. in tests that don't
// need it). Tokens and APIKeys back AuthMiddleware on every route except
// /auth/register and /auth/login (security: [] in the API contract).
type RouterConfig struct {
	AllowedOrigins []string
	// DB backs the /ready probe's dependency check. Nil is allowed (tests
	// that don't need it) — /ready then reports ready without checking.
	DB                Pinger
	IdempotencyStore  IdempotencyStore
	Auth              *AuthHandlers
	Tokens            *auth.TokenIssuer
	APIKeys           APIKeyLookup
	Resources         *ResourceHandlers
	KnowledgeBases    *KnowledgeBaseHandlers
	Agents            *AgentHandlers
	Bundles           *BundleHandlers
	Marketplace       *MarketplaceHandlers
	ModelProviders    *ModelProviderHandlers
	ModelCatalog      *ModelCatalogHandlers
	ModelCatalogAdmin *ModelCatalogAdminHandlers
	Usage             *UsageHandlers
	Runs              *RunHandlers
	Operations        *OperationHandlers
	RBAC              *RBACHandlers
	Features          FeaturesConfig
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
	authHandlers := cfg.Auth

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
		if cfg.DB != nil {
			pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := cfg.DB.Ping(pingCtx); err != nil {
				writeErr(w, r, http.StatusServiceUnavailable, ErrInternal, "database unreachable")
				return
			}
		}
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
				r.Post("/resources/mcp/probe", cfg.Resources.Probe)
				r.Post("/resources/components/import-openapi", cfg.Resources.ImportOpenAPI)
				r.Post("/resources/components/batch", cfg.Resources.BatchCreateComponents)
				r.Post("/resources/skills/upload", cfg.Resources.UploadSkill)
				r.Get("/resources/skills/{id}/files", cfg.Resources.ListSkillFiles)
				r.Get("/resources/skills/{id}/files/*", cfg.Resources.GetSkillFile)
			}
			if cfg.KnowledgeBases != nil {
				r.Post("/resources/{id}/kb/documents", cfg.KnowledgeBases.IngestDocument)
				r.Get("/resources/{id}/kb/documents", cfg.KnowledgeBases.ListDocuments)
				r.Delete("/resources/{id}/kb/documents/{source_ref}", cfg.KnowledgeBases.DeleteDocument)
				r.Post("/resources/{id}/kb/search", cfg.KnowledgeBases.Search)
			}
			if cfg.Agents != nil {
				r.Get("/agents", cfg.Agents.List)
				r.Post("/agents", cfg.Agents.Create)
				r.Get("/agents/{ref}/versions", cfg.Agents.ListVersions)
				r.Delete("/agents/{ref}", cfg.Agents.Delete)
			}
			if cfg.Bundles != nil {
				r.Get("/bundles", cfg.Bundles.List)
				r.Post("/bundles", cfg.Bundles.Create)
				r.Delete("/bundles/{ref}", cfg.Bundles.Delete)
			}
			if cfg.Marketplace != nil {
				r.Get("/marketplace/listings", cfg.Marketplace.Browse)
				r.Post("/marketplace/listings", cfg.Marketplace.Publish)
				r.Get("/marketplace/listings/{ref}", cfg.Marketplace.Detail)
				r.Post("/marketplace/listings/{id}/unpublish", cfg.Marketplace.Unpublish)
				r.Post("/marketplace/listings/{id}/subscribe", cfg.Marketplace.Subscribe)
				r.Get("/marketplace/subscriptions", cfg.Marketplace.ListSubscriptions)
				r.Delete("/marketplace/subscriptions/{id}", cfg.Marketplace.Unsubscribe)
				r.Post("/marketplace/subscriptions/{id}/upgrade", cfg.Marketplace.Upgrade)
			}
			if cfg.Operations != nil {
				r.Get("/audit-logs", cfg.Operations.ListMyAuditLogs)
				r.Post("/marketplace/listings/{ref}/report", cfg.Operations.SubmitReport)
				r.Get("/moderation/reports", cfg.Operations.ListPendingReports)
				r.Post("/moderation/reports/{id}/resolve", cfg.Operations.ResolveReport)
			}
			if cfg.ModelProviders != nil {
				r.Get("/model-providers", cfg.ModelProviders.List)
				r.Post("/model-providers", cfg.ModelProviders.Create)
			}
			if cfg.ModelCatalog != nil {
				r.Get("/model-catalog", cfg.ModelCatalog.List)
			}
			if cfg.ModelCatalogAdmin != nil {
				r.Get("/model-catalog/providers", cfg.ModelCatalogAdmin.ListProviders)
				r.Post("/model-catalog/providers", cfg.ModelCatalogAdmin.CreateProvider)
				r.Patch("/model-catalog/providers/{id}", cfg.ModelCatalogAdmin.UpdateProviderStatus)
				r.Delete("/model-catalog/providers/{id}", cfg.ModelCatalogAdmin.DeleteProvider)
				r.Get("/model-catalog/providers/{id}/models", cfg.ModelCatalogAdmin.ListModels)
				r.Post("/model-catalog/providers/{id}/models", cfg.ModelCatalogAdmin.CreateModel)
				r.Patch("/model-catalog/providers/{id}/models/{model_id}", cfg.ModelCatalogAdmin.UpdateModelStatus)
				r.Delete("/model-catalog/providers/{id}/models/{model_id}", cfg.ModelCatalogAdmin.DeleteModel)
			}
			if cfg.RBAC != nil {
				r.Get("/me/permissions", cfg.RBAC.MyPermissions)
				r.Get("/permissions", cfg.RBAC.ListPermissions)
				r.Get("/roles", cfg.RBAC.ListRoles)
				r.Post("/roles", cfg.RBAC.CreateRole)
				r.Patch("/roles/{id}/permissions", cfg.RBAC.UpdateRolePermissions)
				r.Delete("/roles/{id}", cfg.RBAC.DeleteRole)
				r.Get("/users", cfg.RBAC.ListUsers)
				r.Patch("/users/{id}/status", cfg.RBAC.UpdateUserStatus)
				r.Patch("/users/{id}/roles", cfg.RBAC.UpdateUserRoles)
			}
			r.Get("/features", FeaturesHandler(cfg.Features))
			if cfg.Usage != nil {
				r.Get("/usage/me", cfg.Usage.GetMyUsage)
			}
			if cfg.Runs != nil {
				r.Get("/runs", cfg.Runs.List)
				r.Get("/runs/{id}", cfg.Runs.Get)
				r.Get("/runs/{id}/stream", cfg.Runs.Stream)
				r.Post("/runs/{id}/cancel", cfg.Runs.Cancel)
				r.Post("/runs/{id}/gate", cfg.Runs.ResolveGate)
				// A run can burn a lot of tokens per call, so creation
				// gets its own tighter limiter on top of the general one.
				runCreateLimiter := NewRateLimiter(20, time.Minute, generalRateLimitKey)
				r.With(runCreateLimiter.Middleware).Post("/runs", cfg.Runs.Create)
				// A draft test run burns tokens exactly like a real one, so
				// it sits behind the same tighter limiter.
				r.With(runCreateLimiter.Middleware).Post("/runs/agent-test", cfg.Runs.CreateAgentTest)
			}
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
