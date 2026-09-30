# Dev tools (golangci-lint, sqlc, goose, oapi-codegen) are pinned in backend/go.mod
# as `tool` dependencies and run with `go tool`, so nothing is installed globally.

BACKEND := backend
ENV_FILE := .env

ifneq (,$(wildcard $(ENV_FILE)))
include $(ENV_FILE)
export
endif

.PHONY: db-up db-down migrate migrate-down migrate-status generate run-api build test lint fmt

db-up: ## Start local Postgres
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data volume is kept)
	docker compose down

GOOSE := cd $(BACKEND) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DATABASE_URL)" GOOSE_MIGRATION_DIR=migrations go tool goose

migrate: ## Apply all pending migrations
	$(GOOSE) up

migrate-down: ## Roll back the most recent migration
	$(GOOSE) down

migrate-status: ## Show migration status
	$(GOOSE) status

generate: ## Regenerate sqlc queries and OpenAPI types
	cd $(BACKEND) && go tool sqlc generate && go generate ./...

run-api: ## Run the API server locally
	cd $(BACKEND) && go run ./cmd/api

build: ## Build backend binaries into backend/bin
	cd $(BACKEND) && go build -o bin/ ./cmd/...

test: ## Run backend tests (needs `make db-up`; each test uses a throwaway database)
	cd $(BACKEND) && go test ./...

lint: ## Lint backend code
	cd $(BACKEND) && go tool golangci-lint run ./...

fmt: ## Format backend code
	cd $(BACKEND) && go tool golangci-lint fmt ./...
