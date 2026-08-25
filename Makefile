.PHONY: dev dev-local dev-backend dev-frontend build test lint migrate-up migrate-down migrate-clean sqlc-gen openapi-lint fmt

# Load .env (DATABASE_URL, etc.) into every recipe's environment, so targets
# like migrate-up don't each need their own `set -a; . ./.env; set +a;`.
# `-include` is silent if .env doesn't exist yet (e.g. a fresh checkout
# before `cp .env.example .env`).
-include .env
export

dev:
	docker compose up --build

# Non-Docker local dev: runs the Go server and the Vite dev server directly
# on the host, in parallel, and tears both down together on Ctrl-C.
#
# Assumes a local PostgreSQL is already running and reachable at the
# DATABASE_URL in .env (see .env.example — `createdb agentic_kit` after
# creating the agentic_kit role is enough), and that migrations are applied
# (`make migrate-up`). This target does not manage Postgres itself.
dev-local:
	@trap 'kill 0' EXIT INT TERM; ( set -a; . ./.env; set +a; go run ./cmd/server ) & ( cd web && npm run dev ) & wait

# Run just the backend, reading config from .env (mirrors dev-local's half).
dev-backend:
	set -a; . ./.env; set +a; go run ./cmd/server

# Run just the frontend dev server (Vite proxies /api to localhost:8080 —
# see web/vite.config.ts).
dev-frontend:
	cd web && npm run dev

build:
	go build ./...
	cd web && npm run build

test:
	go test ./...

lint:
	golangci-lint run ./...
	cd web && npx tsc -b --noEmit && npm run lint
	npx --yes @redocly/cli@1.25.11 lint api/openapi.yaml

fmt:
	gofmt -w .

migrate-up:
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1 \
		-path migrations -database "$${DATABASE_URL}" up

migrate-down:
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1 \
		-path migrations -database "$${DATABASE_URL}" down 1

# Clears a "Dirty database version N" state left by a migration that failed
# partway through, so migrate-up can run again — golang-migrate marks the
# version dirty and refuses to proceed until it's told which version the
# database is actually consistent at. VERSION defaults to 12 (the state
# just before the now-deleted pgvector migration 0013), overridable:
# `make migrate-clean VERSION=5`.
VERSION ?= 12
migrate-clean:
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1 \
		-path migrations -database "$${DATABASE_URL}" force $(VERSION)

sqlc-gen:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate
