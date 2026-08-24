.PHONY: dev dev-local dev-backend dev-frontend build test lint migrate-up migrate-down sqlc-gen openapi-lint fmt

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
	go run github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1 \
		-path migrations -database "$${DATABASE_URL}" up

migrate-down:
	go run github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1 \
		-path migrations -database "$${DATABASE_URL}" down 1

sqlc-gen:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate
