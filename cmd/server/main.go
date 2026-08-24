// Command server starts the AI Agent platform HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	adaptercrypto "github.com/marcon0203/agentic-kit/internal/adapter/crypto"
	"github.com/marcon0203/agentic-kit/internal/adapter/mcp"
	adaptermodelgateway "github.com/marcon0203/agentic-kit/internal/adapter/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/adapter/orchestrator"
	"github.com/marcon0203/agentic-kit/internal/adapter/password"
	"github.com/marcon0203/agentic-kit/internal/adapter/postgres"
	adapterschema "github.com/marcon0203/agentic-kit/internal/adapter/schema"
	"github.com/marcon0203/agentic-kit/internal/api"
	"github.com/marcon0203/agentic-kit/internal/auth"
	"github.com/marcon0203/agentic-kit/internal/config"
	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
	"github.com/marcon0203/agentic-kit/internal/domain/iam"
	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
	"github.com/marcon0203/agentic-kit/internal/domain/operation"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	domainrun "github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/observability"
	"github.com/marcon0203/agentic-kit/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, err := observability.Setup(ctx, "agentic-kit-server", "1.0.0")
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.Error("otel_shutdown_failed", "error", err)
		}
	}()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// Registers pgvector's Go <-> `vector` column codec on every pooled
	// connection — required for the knowledge-base embedding columns to
	// scan/bind as pgvector.Vector instead of failing with an unknown OID.
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	aesKey, err := crypto.DecodeKey(cfg.CredentialAESKey)
	if err != nil {
		return fmt.Errorf("decode CREDENTIAL_AES_KEY: %w", err)
	}

	agentValidator, err := dslschema.NewAgentValidator()
	if err != nil {
		return fmt.Errorf("compile agent schema: %w", err)
	}
	bundleValidator, err := dslschema.NewBundleValidator()
	if err != nil {
		return fmt.Errorf("compile bundle schema: %w", err)
	}

	queries := store.New(pool)

	// Domain wiring: repository adapters implement the ports each context
	// declares, services own the rules, handlers only do transport.
	// The catalog is shared: "is this resource usable?" must mean the same
	// thing when an Agent version is published and when a run starts.
	resourceCatalog := postgres.NewResourceCatalog(queries)

	agentService := agent.NewService(
		postgres.NewAgentRepository(queries),
		resourceCatalog,
		adapterschema.NewValidator(agentValidator),
	)

	marketplaceService := marketplace.NewService(
		postgres.NewListingRepository(queries),
		postgres.NewSubscriptionRepository(queries),
		postgres.NewMarketplaceCatalog(queries),
		postgres.NewDependencyValidator(queries),
		postgres.NewUserDirectory(queries),
	)

	bundleService := bundle.NewService(
		postgres.NewBundleRepository(queries),
		postgres.NewAgentHandoffs(queries),
		adapterschema.NewValidator(bundleValidator),
	)

	resourceService := resource.NewService(
		postgres.NewResourceRepository(queries),
		adaptercrypto.NewCipher(aesKey),
		mcp.NewReachabilityProbe(),
	)

	providerKeys := postgres.NewProviderKeyStore(queries, aesKey)

	// A standalone Gateway for calls that happen outside a Bundle run
	// (embedding a document at ingest time, embedding a search query) —
	// Engine builds its own per-run Gateway for completions, but knowledge
	// base ingest/search need one too and aren't part of a run.
	embeddingGateway := modelgateway.NewGateway(nil)
	knowledgeBaseService := knowledgebase.NewService(
		postgres.NewKnowledgeBaseRepository(queries),
		embeddingGateway,
		providerKeys,
		resourceService,
	)

	// The run context is assembled from more parts than the others: it
	// needs persistence, the ADK orchestrator behind it, and the gate
	// registry that both the orchestrator (which blocks on gates) and the
	// timeout scanner (which resolves them) must share.
	runRepo := postgres.NewRunRepository(queries)
	runEvents := postgres.NewRunEventStore(queries)
	gateRepo := postgres.NewGateRepository(queries)
	gateRegistry := orchestrator.NewGateRegistry()

	runService := domainrun.NewService(
		runRepo,
		runEvents,
		postgres.NewRunBundleResolver(queries),
		postgres.NewRunDependencyChecker(queries, resourceCatalog, providerKeys),
		orchestrator.NewEngine(queries, runRepo, runEvents, gateRepo, gateRegistry, providerKeys, aesKey, knowledgeBaseService),
		gateRepo,
		gateRegistry,
		postgres.NewAuditLogWriter(queries),
		orchestrator.NewRunIDGenerator(),
	)

	// Providers and usage are two halves of one context: which providers
	// this user may reach, and what reaching them has cost.
	modelCenter := modelcenter.NewService(
		postgres.NewModelProviderRepository(queries),
		adaptercrypto.NewCipher(aesKey),
		adaptermodelgateway.NewConnectivityChecker(),
		postgres.NewUsageRepository(queries),
	)

	passwordHasher, err := password.NewHasher()
	if err != nil {
		return fmt.Errorf("initialise password hasher: %w", err)
	}
	iamService := iam.NewService(
		postgres.NewUserRepository(queries),
		passwordHasher,
		auth.NewTokenIssuer(cfg.JWTSecret),
	)

	routerCfg := api.RouterConfig{
		AllowedOrigins:   splitAndTrim(cfg.CORSAllowedOrigins),
		DB:               pool,
		IdempotencyStore: api.NewPostgresIdempotencyStore(queries),
		Auth:             api.NewAuthHandlers(iamService),
		Tokens:           auth.NewTokenIssuer(cfg.JWTSecret),
		APIKeys:          api.NewPostgresAPIKeyLookup(queries),
		Resources:        api.NewResourceHandlers(resourceService),
		KnowledgeBases:   api.NewKnowledgeBaseHandlers(knowledgeBaseService),
		Agents:           api.NewAgentHandlers(agentService),
		Bundles:          api.NewBundleHandlers(bundleService),
		Marketplace:      api.NewMarketplaceHandlers(marketplaceService),
		ModelProviders:   api.NewModelProviderHandlers(modelCenter),
		ModelCatalog:     api.NewModelCatalogHandlers(),
		Usage:            api.NewUsageHandlers(modelCenter),
		Runs:             api.NewRunHandlers(runService),
		Operations: api.NewOperationHandlers(operation.NewService(
			postgres.NewReportRepository(queries),
			postgres.NewAuditLogReader(queries),
			postgres.NewAuditLogWriter(queries),
			postgres.NewModerationListings(queries),
			postgres.NewResourceDisabler(queries),
			postgres.NewAdminDirectory(queries),
		)),
	}

	go api.RunGateTimeoutScanner(ctx, runService, logger)

	// otelhttp wraps everything so a span (and its trace ID) exists before
	// api.NewRouter's own middleware chain runs — RequestIDMiddleware reads
	// that trace ID as the request_id (spec-19: "request_id 与 trace_id
	// 统一"), and ADK's own spans for the same request join this trace.
	handler := otelhttp.NewHandler(api.NewRouter(logger, routerCfg), "agentic-kit-server")
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server_starting", "port", cfg.HTTPPort, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("server_stopping")
	return srv.Shutdown(shutdownCtx)
}

// splitAndTrim parses a comma-separated CORS_ALLOWED_ORIGINS value into a
// slice, returning nil for an empty input (NewRouter treats nil as "reflect
// any origin", the local-dev default).
func splitAndTrim(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
