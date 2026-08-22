package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestPostgresContainer_Placeholder verifies the testcontainers +
// PostgreSQL 16 setup used by later data-access tests actually works end
// to end: start a real Postgres, connect with pgx, run SELECT 1.
func TestPostgresContainer_Placeholder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16.4-alpine",
		postgres.WithDatabase("agentic_kit_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	if err != nil {
		t.Skipf("docker unavailable, skipping testcontainers test: %v", err)
	}
	t.Cleanup(func() {
		_ = pgContainer.Terminate(context.Background())
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var result int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("query: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}
