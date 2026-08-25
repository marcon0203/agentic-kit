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

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	adaptercrypto "github.com/marcon0203/agentic-kit/internal/adapter/crypto"
	esstore "github.com/marcon0203/agentic-kit/internal/adapter/elasticsearch"
	"github.com/marcon0203/agentic-kit/internal/adapter/mcp"
	"github.com/marcon0203/agentic-kit/internal/adapter/milvus"
	adaptermodelgateway "github.com/marcon0203/agentic-kit/internal/adapter/modelgateway"
	adapteropenapi "github.com/marcon0203/agentic-kit/internal/adapter/openapi"
	"github.com/marcon0203/agentic-kit/internal/adapter/orchestrator"
	"github.com/marcon0203/agentic-kit/internal/adapter/oss"
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
	"github.com/marcon0203/agentic-kit/internal/domain/modelcatalog"
	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
	"github.com/marcon0203/agentic-kit/internal/domain/operation"
	"github.com/marcon0203/agentic-kit/internal/domain/rbac"
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
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
		postgres.NewResourceRepository(queries, pool),
		adaptercrypto.NewCipher(aesKey),
		mcp.NewReachabilityProbe(),
		cfg.KBEnabled,
	).WithOpenAPIImport(adapteropenapi.NewParser())
	// Skill zip upload needs an object store; a deployment that never sets
	// the OSS_* vars still boots cleanly (WithSkillUploads just never gets
	// called, so UploadSkill/ListSkillFiles/GetSkillFile all return a clear
	// "not configured" error instead of a nil-pointer panic). skillObjectStore
	// is also handed to orchestrator.NewEngine below, so a run-time Skill
	// tool call can fetch its SKILL.md the same way the upload/list/download
	// handlers do.
	var skillObjectStore resource.ObjectStore
	if cfg.OSSEnabled() {
		store, err := oss.New(cfg.OSSEndpoint, cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret, cfg.OSSBucket)
		if err != nil {
			return fmt.Errorf("connect to OSS: %w", err)
		}
		skillObjectStore = store
		resourceService = resourceService.WithSkillUploads(skillObjectStore, postgres.NewSkillFileRepository(queries))
	}

	providerKeys := postgres.NewProviderKeyStore(queries, aesKey)

	// 知识库 (Milvus vector search + Elasticsearch keyword search, fused
	// as 多路召回) is entirely optional — KB_ENABLED gates whether either
	// external store is even dialed. Disabled: knowledgeBaseService and
	// kbHandlers stay nil, orchestrator.NewEngine below already treats a
	// nil kbService as "reject knowledge_base tool calls with a clear
	// error" (see knowledgeBaseSearcher), and router.go's
	// `if cfg.KnowledgeBases != nil` already no-ops the KB routes — no
	// nil-guard needed at either of those call sites.
	var knowledgeBaseService *knowledgebase.Service
	var kbHandlers *api.KnowledgeBaseHandlers
	if cfg.KBEnabled {
		vectorStore, err := milvus.NewStore(ctx, milvus.Config{
			Addr: cfg.MilvusAddr, Username: cfg.MilvusUsername, Password: cfg.MilvusPassword,
		})
		if err != nil {
			return fmt.Errorf("connect to Milvus: %w", err)
		}
		keywordStore, err := esstore.NewStore(ctx, esstore.Config{
			Addr: cfg.ElasticsearchAddr, Username: cfg.ElasticsearchUsername,
			Password: cfg.ElasticsearchPassword, APIKey: cfg.ElasticsearchAPIKey,
		})
		if err != nil {
			return fmt.Errorf("connect to Elasticsearch: %w", err)
		}
		// A standalone Gateway for calls that happen outside a Bundle run
		// (embedding a document at ingest time, embedding a search query) —
		// Engine builds its own per-run Gateway for completions, but
		// knowledge base ingest/search need one too and aren't part of a run.
		embeddingGateway := modelgateway.NewGateway(nil)
		knowledgeBaseService = knowledgebase.NewService(vectorStore, keywordStore, embeddingGateway, providerKeys, resourceService)
		kbHandlers = api.NewKnowledgeBaseHandlers(knowledgeBaseService)
	}

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
		orchestrator.NewEngine(queries, runRepo, runEvents, gateRepo, gateRegistry, providerKeys, aesKey, knowledgeBaseService, skillObjectStore),
		gateRepo,
		gateRegistry,
		postgres.NewAuditLogWriter(queries),
		orchestrator.NewRunIDGenerator(),
	).WithAgentTestRuns(postgres.NewAgentTestBundleProvider(queries))

	// Providers and usage are two halves of one context: which providers
	// this user may reach, and what reaching them has cost.
	modelCenter := modelcenter.NewService(
		postgres.NewModelProviderRepository(queries),
		adaptercrypto.NewCipher(aesKey),
		adaptermodelgateway.NewConnectivityChecker(),
		postgres.NewUsageRepository(queries),
	)

	// 系统配置 → 模型提供商: admin-managed catalog (Provider + its Models),
	// distinct from modelCenter's per-user connected credentials above.
	adminDirectory := postgres.NewAdminDirectory(queries)
	modelCatalog := modelcatalog.NewService(postgres.NewModelCatalogRepository(queries), adminDirectory)

	// 系统配置 → 用户管理 / 角色权限: roles/permissions engine. adminDirectory
	// doubles as its AdminDirectory too — role/permission administration
	// stays is_admin-only (see rbac.Service's doc comment) so a Role
	// holder can never grant themselves more.
	rbacService := rbac.NewService(postgres.NewRBACRepository(queries), adminDirectory)

	passwordHasher, err := password.NewHasher()
	if err != nil {
		return fmt.Errorf("initialise password hasher: %w", err)
	}
	iamService := iam.NewService(
		postgres.NewUserRepository(queries),
		passwordHasher,
		auth.NewTokenIssuer(cfg.JWTSecret),
	)

	// Every admin-only surface (系统配置's 用户管理/角色权限/模型提供商,
	// 运营中心) is unreachable without at least one is_admin account, and
	// no API can create the first one — so the server creates it itself on
	// a fresh install. A no-op once any admin exists.
	superadminEmail := cfg.SuperadminEmail
	if superadminEmail == "" {
		superadminEmail = "admin@agentic-kit.local"
	}
	created, superadminPassword, err := iamService.BootstrapSuperAdmin(ctx, superadminEmail, "超级管理员", cfg.SuperadminPassword)
	if err != nil {
		return fmt.Errorf("bootstrap superadmin: %w", err)
	}
	if created {
		logger.Warn("superadmin_bootstrapped",
			"email", superadminEmail,
			"password", superadminPassword,
			"note", "save this now — it is never logged or shown again",
		)
	}

	routerCfg := api.RouterConfig{
		AllowedOrigins:    splitAndTrim(cfg.CORSAllowedOrigins),
		DB:                pool,
		IdempotencyStore:  api.NewPostgresIdempotencyStore(queries),
		Auth:              api.NewAuthHandlers(iamService),
		Tokens:            auth.NewTokenIssuer(cfg.JWTSecret),
		APIKeys:           api.NewPostgresAPIKeyLookup(queries),
		Resources:         api.NewResourceHandlers(resourceService, mcp.NewProber()),
		KnowledgeBases:    kbHandlers,
		Agents:            api.NewAgentHandlers(agentService),
		Bundles:           api.NewBundleHandlers(bundleService),
		Marketplace:       api.NewMarketplaceHandlers(marketplaceService),
		ModelProviders:    api.NewModelProviderHandlers(modelCenter),
		ModelCatalog:      api.NewModelCatalogHandlers(modelCatalog),
		ModelCatalogAdmin: api.NewModelCatalogAdminHandlers(modelCatalog),
		Usage:             api.NewUsageHandlers(modelCenter),
		Runs:              api.NewRunHandlers(runService),
		RBAC:              api.NewRBACHandlers(rbacService),
		Features:          api.FeaturesConfig{KnowledgeBaseEnabled: cfg.KBEnabled, SkillUploadEnabled: cfg.OSSEnabled()},
		Operations: api.NewOperationHandlers(operation.NewService(
			postgres.NewReportRepository(queries),
			postgres.NewAuditLogReader(queries),
			postgres.NewAuditLogWriter(queries),
			postgres.NewModerationListings(queries),
			postgres.NewResourceDisabler(queries),
			adminDirectory,
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
