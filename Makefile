# Load .env (if present) and pass its values to every command.
-include .env
export

LINT := bin/golangci-lint
REDOCLY := npx -y @redocly/cli@2.57.0
COMPOSE := docker compose
LIQUIBASE := $(COMPOSE) run --rm liquibase

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'

## --- API -----------------------------------------------------------------

.PHONY: run
run: ## Start the API (needs `make db-up migrate` first)
	go run ./cmd/api

.PHONY: build
build: ## Build the API binary into bin/api
	go build -o bin/api ./cmd/api

.PHONY: test
test: ## Run unit tests
	go test -race ./...

.PHONY: test-integration
test-integration: ## Run all tests, including those against a real Postgres (needs Docker)
	go test -race -tags=integration ./...

.PHONY: check
check: lint gen-check tidy-check test-integration ## Everything CI checks; run before every commit (needs Docker)

.PHONY: task
task: ## Run one task's acceptance tests, including pending ones: make task TASK=T03
	@test -n "$(TASK)" || { echo "usage: make task TASK=T01 (see docs/tasks/README.md)"; exit 1; }
	TASK=$(TASK) go test -race -tags=integration -count=1 ./...

.PHONY: gen-check
gen-check: ## Fail if internal/httpapi/gen is out of date with api/openapi.yaml
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	@git diff --exit-code -- internal/httpapi/gen || { echo "Generated code changed: commit it, or undo your edits under internal/httpapi/gen."; exit 1; }

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum need `go mod tidy`
	go mod tidy -diff

.PHONY: lint
lint: $(LINT) ## Run golangci-lint
	$(LINT) run

$(LINT): tools/lint/go.mod tools/lint/go.sum
	go -C tools/lint build -o ../../$(LINT) github.com/golangci/golangci-lint/v2/cmd/golangci-lint

.PHONY: lint-spec
lint-spec: ## Validate api/openapi.yaml (needs Node.js)
	$(REDOCLY) lint api/openapi.yaml --config api/redocly.yaml

.PHONY: gen
gen: ## Generate Go server types and interface from api/openapi.yaml
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

## --- Database --------------------------------------------------------------

.PHONY: db-up
db-up: ## Start local Postgres
	$(COMPOSE) up -d --wait postgres

.PHONY: db-down
db-down: ## Stop local Postgres (keeps data)
	$(COMPOSE) down

.PHONY: db-reset
db-reset: ## Stop local Postgres and delete its data
	$(COMPOSE) down -v

.PHONY: migrate
migrate: ## Apply pending Liquibase migrations
	$(LIQUIBASE) update

.PHONY: migrate-rollback
migrate-rollback: ## Roll back the last migration changeset
	$(LIQUIBASE) rollback-count --count=1

.PHONY: migrate-status
migrate-status: ## Show migrations that have not run yet
	$(LIQUIBASE) status --verbose

.PHONY: psql
psql: ## Open a psql shell on the local database
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER:-blinks} -d $${POSTGRES_DB:-blinks}

.PHONY: seed
seed: ## Load development sample data (M2)
	@echo "make seed arrives in M2 with the catalogue." && exit 1
