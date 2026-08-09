# Scout — one entry point for every routine operation.
#
# The target list is specified in docs/15-infrastructure-deployment.md section 1.
# Targets exist here even when the thing they drive does not yet, because a
# runbook that says `make restore-drill` and a Makefile that does not have it is
# how a 2am procedure fails at step one. Where the underlying thing is missing,
# the target says so and exits non-zero rather than pretending to succeed.
#
# Everything below assumes: docker, and (for deploy and backup) tailscale.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ENV_FILE ?= .env

COMPOSE_LOCAL := docker compose --env-file $(ENV_FILE) -f infra/compose/local.yml
COMPOSE_PROD  := docker compose --env-file $(ENV_FILE) -f infra/compose/production.yml

# The tailnet name of the production host. Overridable so the fallback
# configuration (the MacBook as production) does not need a code change.
SCOUT_HOST ?= scout-host

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --------------------------------------------------------------- local dev ---

$(ENV_FILE):
	@echo "no $(ENV_FILE); copying .env.example"
	@cp .env.example $(ENV_FILE)

.PHONY: dev
dev: $(ENV_FILE) ## Start the full local stack, migrated and ready
	$(COMPOSE_LOCAL) up -d --build --wait
	@$(MAKE) --no-print-directory migrate
	@echo
	@echo "  api      http://127.0.0.1:$${SCOUT_API_PORT:-8081}/health"
	@echo "  postgres postgres://scout@127.0.0.1:$${SCOUT_PG_PORT:-5433}/scout"
	@echo "  redis    redis://127.0.0.1:$${SCOUT_REDIS_PORT:-6380}/0"

.PHONY: dev-db
dev-db: $(ENV_FILE) ## Start only Postgres and Redis, for running one service natively
	$(COMPOSE_LOCAL) up -d --wait postgres redis
	@$(MAKE) --no-print-directory migrate

.PHONY: down
down: ## Stop the local stack, keeping data
	$(COMPOSE_LOCAL) down

.PHONY: clean
clean: ## Stop the local stack and delete its data
	$(COMPOSE_LOCAL) down --volumes

.PHONY: logs
logs: ## Follow local stack logs
	$(COMPOSE_LOCAL) logs -f

# -------------------------------------------------------------- migrations ---

# golang-migrate runs from its own image rather than as a Go dependency
# (docs/15 section 3). Nothing in the repository imports it, so vendoring it into
# go.mod would add a dependency tree to three service binaries for the sake of a
# command that runs at deploy time — and P0 adds tooling only when something
# needs it. Same binary, same version, local and production.
.PHONY: migrate
migrate: $(ENV_FILE) ## Apply pending migrations to the local database
	$(COMPOSE_LOCAL) --profile tools run --rm migrate up

.PHONY: migrate-status
migrate-status: $(ENV_FILE) ## Show the current local schema version
	$(COMPOSE_LOCAL) --profile tools run --rm migrate version

# sqlc runs from the pinned sqlc/sqlc image for the same reason migrate does:
# reproducible codegen without every contributor's local sqlc binary having to
# match. AGENTS.md: "Queries via sqlc — never string-concatenated SQL." Output
# is committed to packages/db/gen, so `go build` never needs sqlc or Docker at
# all — only regenerating after a schema or query change does.
SQLC_IMAGE := sqlc/sqlc:1.31.1

.PHONY: db-generate
db-generate: ## Regenerate packages/db/gen from the schema and queries
	docker run --rm -v "$(CURDIR)/packages/db:/src" -v "$(CURDIR)/infra/migrations:/src/../../infra/migrations:ro" \
		-w /src $(SQLC_IMAGE) generate
	@echo "regenerated packages/db/gen — review the diff before committing"

.PHONY: db-verify
db-verify: ## Fail if packages/db/gen is stale relative to the schema and queries
	@tmp=$$(mktemp -d); cp -r packages/db/gen "$$tmp/before"; \
	$(MAKE) --no-print-directory db-generate >/dev/null; \
	if ! diff -rq "$$tmp/before" packages/db/gen >/dev/null 2>&1; then \
		echo "packages/db/gen is stale — run 'make db-generate' and commit the result" >&2; \
		rm -rf "$$tmp"; exit 1; \
	fi; \
	rm -rf "$$tmp"; echo "ok: packages/db/gen matches the schema and queries"

# ------------------------------------------------------------------- tests ---

.PHONY: test
test: ## Run all tests, all languages
	go test -count=1 ./...
	@# Python (P2) and TypeScript (P3) test invocations join this target with the
	@# milestone that introduces them. Adding empty ones now would make a green
	@# `make test` mean less than it does today.

.PHONY: lint
lint: lint-go lint-sql ## Run every linter

.PHONY: lint-go
lint-go: ## Run golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 \
		|| { echo "golangci-lint not installed: brew install golangci-lint"; exit 1; }
	golangci-lint run

.PHONY: lint-sql
lint-sql: ## Run sqlfluff over migrations and application queries
	@command -v sqlfluff >/dev/null 2>&1 \
		|| { echo "sqlfluff not installed: pipx install 'sqlfluff==3.*'"; exit 1; }
	sqlfluff lint infra/migrations --dialect postgres
	@# Application queries use a separate config: lowercase keywords, and LT01
	@# stays on because they are not column-aligned. See .sqlfluff.
	@if compgen -G "packages/db/queries/*.sql" >/dev/null; then \
		sqlfluff lint packages/db/queries --config .sqlfluff-queries --dialect postgres; \
	else \
		echo "no application queries yet; skipping .sqlfluff-queries"; \
	fi

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w apps packages

.PHONY: compliance
compliance: ## Run the banned-dependency gate (AGENTS.md rule 1a)
	node infra/scripts/check-banned-deps.mjs

# The four targets below are referenced by docs/15 and by the runbooks, and do
# not exist yet. They fail loudly with a pointer to the milestone that builds
# them, rather than being absent — a runbook that fails at step one with
# "make: no rule to make target" tells you nothing, and a target that prints
# "not yet" and exits 0 is worse, because a script calling it believes it worked.

.PHONY: evals
evals: ## Run the quality eval harness
	@echo "no eval harness yet — it lands with the dedup and scoring work at P2."
	@echo "see docs/17-testing-qa.md and evals/README.md."
	@exit 1

.PHONY: evals-report
evals-report: ## Per-suite eval diff against the last passing run
	@echo "no eval harness yet — see docs/runbooks/quality-regression.md, which"
	@echo "references this target, and docs/17-testing-qa.md. Lands at P2."
	@exit 1

.PHONY: fixtures
fixtures: ## Re-record adapter fixtures (requires network and approval)
	@echo "no adapters yet — fixtures land with the first adapter at P1."
	@echo "see docs/06-ingestion-pipeline.md and adapters/README.md."
	@exit 1

.PHONY: fixtures-diff
fixtures-diff: ## Diff a source's live response against its recorded fixture
	@echo "no adapters yet — see docs/runbooks/source-broken.md, which references"
	@echo "this target. Lands with the first adapter at P1."
	@exit 1

# ------------------------------------------------------------------ deploy ---

.PHONY: deploy
deploy: ## Build and deploy to production over Tailscale SSH
	SCOUT_HOST=$(SCOUT_HOST) infra/scripts/deploy.sh

.PHONY: prod-logs
prod-logs: ## Follow production logs over Tailscale SSH
	ssh $(SCOUT_HOST) 'cd /opt/scout && docker compose -f infra/compose/production.yml logs -f --tail=100'

.PHONY: prod-ps
prod-ps: ## Show production container status
	ssh $(SCOUT_HOST) 'cd /opt/scout && docker compose -f infra/compose/production.yml ps'

.PHONY: health-gate
health-gate: ## Run the deploy health gate against production
	ssh $(SCOUT_HOST) 'cd /opt/scout && ./infra/scripts/health-gate.sh --timeout 60'

.PHONY: tailscale-serve
tailscale-serve: ## (Run on the host) point the tailnet name at Caddy
	@echo "Run this ON the production host, once, after the first deploy:"
	@echo
	@echo "  sudo tailscale serve --bg --https=443 http://172.28.0.10:8080"
	@echo
	@echo "It survives reboots. Verify with: tailscale serve status"
	@echo "See infra/caddy/Caddyfile for why the target is a container address."

# ------------------------------------------------------------------ backup ---

.PHONY: backup-now
backup-now: ## Force an immediate irreplaceable-data backup on the host
	ssh $(SCOUT_HOST) 'cd /opt/scout && ./infra/scripts/backup.sh irreplaceable'

.PHONY: backup-full
backup-full: ## Force an immediate full backup on the host
	ssh $(SCOUT_HOST) 'cd /opt/scout && ./infra/scripts/backup.sh full'

.PHONY: restore-drill
restore-drill: ## Restore the latest nightly into a throwaway local container
	infra/scripts/restore-drill.sh
