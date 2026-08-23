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

	"github.com/marcon0203/agentic-kit/internal/api"
	"github.com/marcon0203/agentic-kit/internal/auth"
	"github.com/marcon0203/agentic-kit/internal/config"
	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
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
	gates := api.NewGateRegistry()
	runEngine := api.NewRunEngine(queries, aesKey, gates, api.NewResourceRefChecker(queries))

	routerCfg := api.RouterConfig{
		AllowedOrigins:   splitAndTrim(cfg.CORSAllowedOrigins),
		DB:               pool,
		IdempotencyStore: api.NewPostgresIdempotencyStore(queries),
		Users:            api.NewPostgresAuthUserStore(queries),
		Tokens:           auth.NewTokenIssuer(cfg.JWTSecret),
		APIKeys:          api.NewPostgresAPIKeyLookup(queries),
		Resources:        api.NewResourceHandlers(queries, api.NewHTTPReachabilityChecker(), aesKey),
		Agents:           api.NewAgentHandlers(queries, agentValidator, api.NewResourceRefChecker(queries)),
		Bundles:          api.NewBundleHandlers(queries, bundleValidator),
		Marketplace:      api.NewMarketplaceHandlers(queries),
		ModelProviders:   api.NewModelProviderHandlers(queries, aesKey),
		Usage:            api.NewUsageHandlers(queries),
		Runs:             api.NewRunHandlers(queries, runEngine),
		Operations:       api.NewOperationHandlers(queries),
	}

	go api.RunGateTimeoutScanner(ctx, queries, gates, logger)

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
