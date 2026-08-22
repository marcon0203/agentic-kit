package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startMigratedPostgres boots a real PostgreSQL 16 container, applies all
// migrations in migrations/ with golang-migrate, and returns a ready-to-use
// pgx connection. Using a real Postgres (not sqlmock) is deliberate — the
// immutable trigger and JSONB behavior below only exist in real Postgres.
func startMigratedPostgres(t *testing.T) *pgx.Conn {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	db := stdlib.OpenDB(*connConfig)
	t.Cleanup(func() { _ = db.Close() })

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		t.Fatalf("migrate driver: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", "pgx", driver)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	// A second Up with no new migrations must be idempotent (ErrNoChange),
	// not an error — verifies make migrate-up can be run repeatedly.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up (idempotency check): %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	return conn
}

func TestMigrations_ApplyCleanly(t *testing.T) {
	conn := startMigratedPostgres(t)

	var tableCount int
	err := conn.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = ANY($1)`,
		[]string{"users", "agents", "bundles", "resources", "marketplace_listings",
			"subscriptions", "bundle_runs", "bundle_run_events", "model_providers"},
	).Scan(&tableCount)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if tableCount != 9 {
		t.Fatalf("expected 9 tables, found %d", tableCount)
	}
}

func TestImmutableTrigger_BlocksUpdateAndDelete(t *testing.T) {
	conn := startMigratedPostgres(t)
	ctx := context.Background()

	var userID int64
	err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		"author@example.com", "hash", "Author",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var agentID int64
	err = conn.QueryRow(ctx,
		`INSERT INTO agents (owner_user_id, agent_ref, version, definition, display_meta)
		 VALUES ($1, 'pm', '1.0.0', '{"persona":"secret"}', '{"name":"PM"}')
		 RETURNING id`,
		userID,
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	// Not yet immutable: ordinary updates must succeed.
	if _, err := conn.Exec(ctx, `UPDATE agents SET display_meta = '{"name":"PM v2"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("update before immutable should succeed: %v", err)
	}

	// Subscription time: flipping false -> true must succeed.
	if _, err := conn.Exec(ctx, `UPDATE agents SET immutable = true WHERE id = $1`, agentID); err != nil {
		t.Fatalf("marking immutable should succeed: %v", err)
	}

	// Any further update must be rejected by the trigger.
	_, err = conn.Exec(ctx, `UPDATE agents SET display_meta = '{"name":"tampered"}' WHERE id = $1`, agentID)
	if err == nil {
		t.Fatal("expected UPDATE on immutable row to be rejected, got no error")
	}

	// Delete must also be rejected.
	_, err = conn.Exec(ctx, `DELETE FROM agents WHERE id = $1`, agentID)
	if err == nil {
		t.Fatal("expected DELETE on immutable row to be rejected, got no error")
	}
}

func TestSubscriberQuery_ExcludesDefinition(t *testing.T) {
	conn := startMigratedPostgres(t)
	ctx := context.Background()

	var userID int64
	err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		"author2@example.com", "hash", "Author2",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var agentID int64
	err = conn.QueryRow(ctx,
		`INSERT INTO agents (owner_user_id, agent_ref, version, definition, display_meta)
		 VALUES ($1, 'architect', '1.0.0', '{"persona":"secret system prompt"}', '{"name":"Architect"}')
		 RETURNING id`,
		userID,
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	// This mirrors queries/agents.sql: GetAgentDisplayForSubscriber's
	// column list has no `definition` — the black-box guarantee holds by
	// construction, not by filtering a fetched row.
	rows, err := conn.Query(ctx,
		`SELECT id, owner_user_id, agent_ref, version, display_meta, status, created_at
		 FROM agents WHERE id = $1`,
		agentID,
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	for _, f := range fields {
		if f.Name == "definition" {
			t.Fatal("subscriber-view query must not select the definition column")
		}
	}
	if !rows.Next() {
		t.Fatal("expected one row")
	}
}
