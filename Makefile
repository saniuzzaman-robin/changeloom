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

# Dev Postgres: "docker" (the docker-compose.yml service) or "local" (an installed server that
# DATABASE_URL points at).
POSTGRES_MODE ?= docker
ifeq ($(filter $(POSTGRES_MODE),docker local),)
$(error POSTGRES_MODE must be docker or local, got "$(POSTGRES_MODE)")
endif

ifeq ($(POSTGRES_MODE),local)
PSQL := psql "$(DATABASE_URL)"
else
PSQL := docker compose exec -T postgres psql -U "$(POSTGRES_USER)" -d postgres
endif

# Hosted environment for migrate-remote, deploy-api and the android-* release targets. Prod only
# when asked for explicitly: `make deploy-api DEPLOY_ENV=prod`. See deploy/README.md.
DEPLOY_ENV ?= staging
ifeq ($(filter $(DEPLOY_ENV),staging prod),)
$(error DEPLOY_ENV must be staging or prod, got "$(DEPLOY_ENV)")
endif
# The Android flavor name as it appears in Gradle task names.
DEPLOY_FLAVOR := $(if $(filter prod,$(DEPLOY_ENV)),Prod,Staging)

MOBILE := mobile
# Gradle for the android-* targets; use GRADLE=gradle when the wrapper download fails.
GRADLE ?= ./gradlew

.PHONY: db-up db-down migrate migrate-down migrate-status migrate-remote generate run-api build test lint fmt \
	curator-db curator-migrate curator-seed curator-generate curator-build curator-test curator-lint curator-fmt \
	deploy-api android-apk android-bundle

db-up: ## Start the dev Postgres (docker), or check the installed one is reachable (local)
ifeq ($(POSTGRES_MODE),local)
	@$(PSQL) -tAc 'SELECT 1' >/dev/null || { echo "Postgres at DATABASE_URL is unreachable: start your installed server and create the role and database (see .env.example)"; exit 1; }
else
	docker compose up -d --wait postgres
endif

db-down: ## Stop the dev Postgres (docker; data volume is kept)
ifeq ($(POSTGRES_MODE),local)
	@echo "POSTGRES_MODE=local: nothing to stop; manage your installed Postgres with its own tools"
else
	docker compose down
endif

GOOSE := cd $(BACKEND) && GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DATABASE_URL)" GOOSE_MIGRATION_DIR=migrations go tool goose

migrate: ## Apply all pending migrations
	$(GOOSE) up

migrate-down: ## Roll back the most recent migration
	$(GOOSE) down

migrate-status: ## Show migration status
	$(GOOSE) status

migrate-remote: ## Apply pending backend migrations to DEPLOY_ENV's hosted DB (REMOTE_DATABASE_URL_<ENV> in curator/.env)
	$(CURATOR_RUN) migrate --remote --env $(DEPLOY_ENV)

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
	@$(PSQL) -tAc "SELECT 1 FROM pg_database WHERE datname = '$(CURATOR_DB)'" | grep -q 1 \
		|| $(PSQL) -c 'CREATE DATABASE "$(CURATOR_DB)"'

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

deploy-api: ## Build and deploy the api to Cloud Run for DEPLOY_ENV (reads deploy/<env>.env; run migrate-remote first)
	deploy/deploy-api.sh $(DEPLOY_ENV)

android-apk: ## Build the signed release APK for DEPLOY_ENV (androidApp/build/outputs/apk/<env>/release)
	cd $(MOBILE) && $(GRADLE) :androidApp:assemble$(DEPLOY_FLAVOR)Release

android-bundle: ## Build the signed release AAB for DEPLOY_ENV, for Play Console (androidApp/build/outputs/bundle/<env>Release)
	cd $(MOBILE) && $(GRADLE) :androidApp:bundle$(DEPLOY_FLAVOR)Release
