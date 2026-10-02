# Dev tools (golangci-lint, sqlc, goose, oapi-codegen) are pinned in backend/go.mod
# as `tool` dependencies and run with `go tool`, so nothing is installed globally.

BACKEND := backend
CURATOR := curator
ENV_FILE := .env
# The curator's settings (copy curator/.env.example).
CURATOR_ENV_FILE := $(CURATOR)/.env
# Must match the database in LOCAL_DATABASE_URL.
CURATOR_DB := changeloom_curator

ifneq (,$(wildcard $(ENV_FILE)))
include $(ENV_FILE)
export
endif

ifneq (,$(wildcard $(CURATOR_ENV_FILE)))
include $(CURATOR_ENV_FILE)
export
endif

.PHONY: db-up db-down migrate migrate-down migrate-status migrate-remote generate run-api build test lint fmt \
	curator-db curator-migrate curator-seed curator-generate curator-build curator-test curator-lint curator-fmt

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

migrate-remote: ## Apply pending migrations to the hosted DB (REMOTE_DATABASE_URL: Neon's direct, non-pooled URL)
	@test -n "$(REMOTE_DATABASE_URL)" || { echo "REMOTE_DATABASE_URL is not set (use Neon's direct, non-pooled URL)"; exit 1; }
	cd $(BACKEND) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(REMOTE_DATABASE_URL)" GOOSE_MIGRATION_DIR=migrations go tool goose up

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

CURATOR_RUN := cd $(CURATOR) && go run ./cmd/curator

curator-db: ## Create the curator's local database on the dev Postgres (needs `make db-up`)
	@docker compose exec -T postgres psql -U "$(POSTGRES_USER)" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$(CURATOR_DB)'" | grep -q 1 \
		|| docker compose exec -T postgres createdb -U "$(POSTGRES_USER)" "$(CURATOR_DB)"

curator-migrate: ## Apply the backend and curator migrations to the curator's local DB
	$(CURATOR_RUN) migrate

curator-seed: ## Load curator/seed/catalog.yaml into the curator's local DB
	$(CURATOR_RUN) seed

curator-generate: ## Regenerate the curator's sqlc queries (rerun after backend migrations change)
	cd $(CURATOR) && go tool sqlc generate

curator-build: ## Build the curator binary into curator/bin
	cd $(CURATOR) && go build -o bin/ ./cmd/...

curator-test: ## Run curator tests (needs `make db-up`; each test uses a throwaway database)
	@cd $(CURATOR) && TEST_DATABASE_URL="$${TEST_DATABASE_URL:-$(DATABASE_URL)}" go test ./...

curator-lint: ## Lint curator code
	cd $(CURATOR) && go tool golangci-lint run ./...

curator-fmt: ## Format curator code
	cd $(CURATOR) && go tool golangci-lint fmt ./...
