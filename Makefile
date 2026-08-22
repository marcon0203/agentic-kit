.PHONY: dev build test lint migrate-up migrate-down sqlc-gen openapi-lint fmt

dev:
	docker compose up --build

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
