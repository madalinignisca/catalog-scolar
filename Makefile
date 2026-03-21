.PHONY: dev dev-api dev-web test test-api test-web lint lint-api lint-web \
       migrate migrate-down migrate-status sqlc seed build clean help \
       check security fix hooks-install

# ── Config ──────────────────────────────────────────────────
DATABASE_URL ?= postgres://catalogro:catalogro@localhost:5432/catalogro?sslmode=disable
REDIS_URL    ?= redis://localhost:6379/0
GOOSE_DIR     = api/db/migrations
SEED_FILE     = api/db/seed.sql

# ── Development ─────────────────────────────────────────────
dev: ## Start all services + API + web dev servers
	docker compose up -d
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U catalogro > /dev/null 2>&1; do sleep 1; done
	@echo "Services ready. Starting API and web..."
	$(MAKE) migrate
	@trap 'kill 0' INT; \
		(cd api && go run ./cmd/server) & \
		(cd web && npm run dev) & \
		wait

dev-api: ## Start only API server
	cd api && go run ./cmd/server

dev-web: ## Start only Nuxt dev server
	cd web && npm run dev

# ── Database ────────────────────────────────────────────────
migrate: ## Run all pending migrations
	cd api && goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down: ## Rollback last migration
	cd api && goose -dir db/migrations postgres "$(DATABASE_URL)" down

migrate-status: ## Show migration status
	cd api && goose -dir db/migrations postgres "$(DATABASE_URL)" status

migrate-create: ## Create new migration (usage: make migrate-create NAME=add_schedule)
	cd api && goose -dir db/migrations create $(NAME) sql

sqlc: ## Regenerate sqlc Go code from queries
	cd api && sqlc generate

seed: ## Load seed data (2 test schools)
	psql "$(DATABASE_URL)" -f $(SEED_FILE)
	@echo "Seed data loaded."

# ── Testing ─────────────────────────────────────────────────
test: test-api test-web ## Run all tests

test-api: ## Run Go tests
	cd api && go test ./... -v -race -count=1

test-web: ## Run Nuxt tests
	cd web && npm run test

# ── Linting ─────────────────────────────────────────────────
lint: lint-api lint-web ## Run all linters

lint-api: ## Run golangci-lint
	cd api && golangci-lint run ./...

lint-web: ## Run ESLint + Prettier check
	cd web && npm run lint

# ── Quality & Security ─────────────────────────────────────
check: ## Run all quality checks (same as pre-commit hooks)
	gitleaks detect --source . --config .gitleaks.toml --no-git -v
	npx editorconfig-checker -exclude '.git|node_modules|.nuxt|.output|bin'
	hadolint api/Dockerfile web/Dockerfile
	cd web && npx prettier --check .
	cd web && npx eslint .
	cd api && golangci-lint run ./...
	cd api && govulncheck ./...
	cd web && npm audit --audit-level=high
	helm lint helm/catalogro
	semgrep --config .semgrep.yml api/

security: ## Run security-focused checks only
	cd api && golangci-lint run --enable-only gosec ./...
	cd api && govulncheck ./...
	cd web && npm audit --audit-level=high
	gitleaks detect --source . --config .gitleaks.toml --no-git -v
	semgrep --config .semgrep.yml api/

fix: ## Auto-fix formatting and lint issues
	cd web && npx prettier --write .
	cd web && npx eslint --fix .
	cd api && find . -name '*.go' -not -path './db/generated/*' | xargs goimports -w

hooks-install: ## Install pre-commit hooks (run once after clone)
	pre-commit install
	@echo "Pre-commit hooks installed. Run 'npm install' in web/ if not done."
	@echo "Run 'go mod tidy' in api/ if go.sum is missing."

# ── Build ───────────────────────────────────────────────────
build: build-api build-web ## Build API + Web

build-api: ## Build Go binary
	cd api && CGO_ENABLED=0 go build -o ../bin/catalogro-api ./cmd/server

build-web: ## Build Nuxt for production
	cd web && npm run build

# ── Docker ──────────────────────────────────────────────────
docker-build: ## Build Docker images
	docker build -t catalogro-api:dev -f api/Dockerfile api/
	docker build -t catalogro-web:dev -f web/Dockerfile web/

# ── Utilities ───────────────────────────────────────────────
clean: ## Stop services, remove volumes
	docker compose down -v
	rm -rf bin/ api/db/generated/ web/.nuxt/ web/.output/

psql: ## Open psql shell
	psql "$(DATABASE_URL)"

redis-cli: ## Open redis-cli
	redis-cli

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
