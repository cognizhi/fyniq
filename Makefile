# Fyniq: the single entry point for local work and CI (PRD-001 §12.8, ADR-T4).
#
# The local-mode command contract: setup, dev, up, down, seed, reset, test, test-e2e, evals,
# images, ci. None of them needs a cloud credential (DEV-02). See .claude/skills/fyniq-local-dev.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

COMPOSE := docker compose -f deploy/local/compose.yaml
DESIGN := docs/design-system/fyniq
SCREENS := docs/design/screens
# OpenSpec CLI: the installed one, else the pinned version through npx (CI)
OPENSPEC ?= $(shell command -v openspec >/dev/null 2>&1 && echo openspec || echo npx -y @fission-ai/openspec@1.14.1)
export OPENSPEC_TELEMETRY := 0
# The pinned toolchain from mise.toml when mise is installed; the tools on PATH otherwise
MISE := $(shell command -v mise >/dev/null 2>&1 && echo "mise exec --")
GO_IMAGES := api ai mcp worker executor render migrate jobs
# Mutation testing scope (QA-ENG-04): money, the rounding policies and the posting engine
MUTATION_PKGS := internal/domain/money internal/domain/rounding internal/domain/ledger
FLAVOURS ?= release local
VERSION := $(shell tr -d '[:space:]' < VERSION)
GIT_SHA := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)

.PHONY: help setup setup-code super-admin-recovery dev up down seed seed-load reset test mutation differential scan db-lint sqlc openapi api-check scan-waivers controls infra-check image-scan image-sign oracle-test test-e2e evals images ci check check-tokens check-config kb-index kb-lint kb-stale spec-check spec-map spec-stale design-kit design-check design-status hooks lint-commits deps-up deps-down deps-logs prototype

help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z0-9-]*:.*## / {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install the pinned toolchain (mise), the web dependencies and the git hooks; write .env
	@if command -v mise >/dev/null 2>&1; then mise install; else echo "⚠ mise is not installed: using the tools on PATH (pinned versions in mise.toml)"; fi
	@[ -f .env ] || { cp .env.example .env; echo "✔ wrote .env from .env.example"; }
	@mkdir -p .data/blobs .data/seed .data/dev
	@$(MISE) go mod download
	@$(MISE) pnpm install --frozen-lockfile
	@$(MAKE) --no-print-directory hooks

setup-code: ## Create the one-time first-run setup code and print it once (FR-ADM-06); refused once setup is done
	@$(MISE) go run -tags local ./cmd/migrate setup-code

super-admin-recovery: ## The deployment's two-person recovery for a Super admin: USER_ID=<user id> OPERATORS="<name>, <name>" (FR-ADM-09)
	@test -n "$(USER_ID)" -a -n "$(OPERATORS)" || { echo 'usage: make super-admin-recovery USER_ID=<user id> OPERATORS="<name>, <name>"'; exit 2; }
	@$(MISE) go run -tags local ./cmd/migrate super-admin-recovery "$(USER_ID)" "$(OPERATORS)"

dev: ## Run the stack with hot reload: dependencies in Docker, the Go services and Vite on the host
	@$(MAKE) --no-print-directory deps-up
	@$(MISE) go run -tags local ./cmd/migrate up
	@$(MAKE) --no-print-directory seed
	@$(MISE) scripts/dev.sh

up: ## Run the stack from the local flavour of the images, behind Caddy on http://localhost:8080
	@$(MAKE) --no-print-directory images FLAVOURS=local
	@$(COMPOSE) --profile core up -d --wait
	@echo "Fyniq       http://localhost:$${FYNIQ_HTTP_PORT:-8080}   (API http://localhost:$${FYNIQ_API_PORT:-8081}/version)"

down: ## Stop the stack started by `make up` (keeps the database volume)
	@$(COMPOSE) --profile core down

seed: ## Write the sample company (DEV-04) to .data/seed/company.json
	@$(MISE) go run -tags local ./cmd/seed --out .data/seed/company.json

seed-load: ## Load the sample company (DEV-04) into an organisation of the local database: ORG=<organisation id>; checks its trial balance against the seed
	@test -n "$(ORG)" || { echo 'usage: make seed-load ORG=<organisation id> (organisation_id in GET /api/v1/session)'; exit 2; }
	@$(MISE) go run -tags local ./cmd/seed --load "$(ORG)"

reset: ## Stop everything and delete local data: the database volume and .data/
	@$(COMPOSE) --profile core down -v
	@rm -rf .data
	@echo "✔ local data removed; run make dev to start again"

test: ## Run every merge gate offline (Docker needed for the database suites): migrations, Go in both flavours, web, the API contract, the oracle, make check
	@$(MAKE) --no-print-directory db-lint
	@$(MISE) go vet ./...
	@$(MISE) go vet -tags local ./...
	@$(MISE) go test ./...
	@$(MISE) go test -tags local ./...
	@$(MISE) pnpm -r typecheck
	@$(MAKE) --no-print-directory api-check
	@$(MAKE) --no-print-directory oracle-test
	@$(MAKE) --no-print-directory check

db-lint: ## Lint the migrations (squawk) and check the adapter's generated queries match them (sqlc)
	@$(MISE) squawk db/migrations/*.sql
	@cd internal/adapters/postgres && $(MISE) sqlc diff || { echo "✖ internal/adapters/postgres/sqlcdb is stale: run make sqlc"; exit 1; }
	@echo "✔ migrations lint clean; generated queries up to date"

mutation: ## Mutation tests on money, rounding and the engine (QA-ENG-04): efficacy of at least 90% per package; part of make ci
	@for p in $(MUTATION_PKGS); do echo "== $$p"; \
	  $(MISE) gremlins unleash ./$$p --workers 4 --timeout-coefficient 6 --threshold-efficacy 90 || exit 1; done

differential: ## The full differential run (QA-ENG-02): Hypothesis looks for a scenario where the engine and the oracle disagree, and saves the shrunk one as a golden case
	@mkdir -p .data/bin && $(MISE) go build -tags local -o .data/bin/scenario-run ./cmd/scenario-run
	@cd oracle && FYNIQ_SCENARIO_RUN=$(CURDIR)/.data/bin/scenario-run $(MISE) uv run --locked python -m pytest -q -p no:cacheprovider ../test/differential/test_engine_agrees.py

scan: ## Security scans (SEC-21): gosec, Semgrep, gitleaks, grype, Checkov, licences, actionlint, zizmor; waivers in compliance/scan-waivers.toml; reports in .data/scan/
	@$(MISE) python3 scripts/scan.py

oracle-test: ## Run the independent oracle's own tests: hand-worked journals, the scenario schemas, Hypothesis properties (QA-ENG-01)
	@cd oracle && $(MISE) uv run --locked python -m pytest -q

sqlc: ## Regenerate the PostgreSQL adapter's typed queries from db/migrations and its queries/
	@cd internal/adapters/postgres && $(MISE) sqlc generate && echo "✔ internal/adapters/postgres/sqlcdb regenerated"

openapi: ## Regenerate the API contract contracts/openapi/fyniq.v1.yaml, its JSON Schemas, the API reference and llms.txt from the code, and the web app's client; review the diff
	@$(MISE) go test ./internal/api -run 'TestTheContractIsTheCode|TestTheReferenceIsTheContract' -count=1 -args -update && echo "✔ contracts/openapi/fyniq.v1.yaml, contracts/schemas/ and docs/api/ regenerated"
	@$(MISE) pnpm --filter @fyniq/web -s gen:api >/dev/null && echo "✔ apps/web/src/api/schema.d.ts regenerated"

api-check: ## The API contract: Spectral lint (NFR-API-01) and the web app's generated client is current (FR-INT-17); oasdiff runs in the Go tests (QA-API-02)
	@$(MISE) pnpm exec spectral lint contracts/openapi/fyniq.v1.yaml --ruleset contracts/openapi/spectral.yaml --fail-severity warn --quiet \
	  && echo "✔ the API contract passes Spectral"
	@$(MISE) pnpm --filter @fyniq/web -s check:api >/dev/null || { echo "✖ apps/web/src/api/schema.d.ts is stale: run pnpm --filter @fyniq/web gen:api"; exit 1; }
	@echo "✔ the web app's API client matches the contract"

test-e2e: ## Run the Playwright journeys (P0: the web shell smoke test)
	@$(MISE) pnpm --filter @fyniq/web test:e2e

evals: ## Run the AI eval suites in replay mode (the suites arrive in P1)
	@scripts/evals.sh

images: ## Build the Go and web images (FLAVOURS="release local" by default) and check them: budgets, non-root, no shell, no local adapter in release (§12.9)
	@for svc in $(GO_IMAGES); do for f in $(FLAVOURS); do \
	  tag=$$( [ "$$f" = local ] && echo local || echo "$(VERSION)" ); \
	  docker build -q -f deploy/docker/Dockerfile.go-service --build-arg SERVICE=$$svc --build-arg FLAVOUR=$$f \
	    --build-arg VERSION=$(VERSION) --build-arg GIT_SHA=$(GIT_SHA) -t fyniq-$$svc:$$tag . >/dev/null; \
	  echo "✔ fyniq-$$svc:$$tag"; done; done
	@docker build -q -f deploy/docker/Dockerfile.web -t fyniq-web:local . >/dev/null && echo "✔ fyniq-web:local"
	@python3 scripts/images.py check --version $(VERSION) --flavours "$(FLAVOURS)"

infra-check: ## OpenTofu in infra/: fmt, init from the lock files, validate, the module tests on mock providers, and no keys, authoritative IAM, human grants or regions outside Singapore (NFR-PORT-05); downloads the providers
	@$(MISE) python3 scripts/infra.py check

image-scan: ## SBOM (syft) and vulnerability scan (grype) of the release images; a critical finding fails unless waived (SEC-21)
	@$(MISE) python3 scripts/images.py scan --version $(VERSION)

image-sign: ## Push the release images by digest to a throwaway registry, sign them and attest their SBOMs (cosign), verify; a wrong key and an unsigned image must fail (NFR-IMG-05, SEC-22)
	@$(MISE) python3 scripts/images.py sign --version $(VERSION)

ci: ## Run what CI runs, in the same order: test, mutation, differential, scan, infra-check, images, image-scan, image-sign, test-e2e, evals
	@$(MAKE) --no-print-directory test
	@$(MAKE) --no-print-directory mutation
	@$(MAKE) --no-print-directory differential
	@$(MAKE) --no-print-directory scan
	@$(MAKE) --no-print-directory infra-check
	@$(MAKE) --no-print-directory images
	@$(MAKE) --no-print-directory image-scan
	@$(MAKE) --no-print-directory image-sign
	@$(MAKE) --no-print-directory test-e2e
	@$(MAKE) --no-print-directory evals

check: check-tokens check-config kb-lint spec-check roadmap-check design-check review-check scan-waivers controls ## Run every check available today (CI runs this)

controls: ## The control registry (compliance/controls/) has a file for every Appendix F control, with the PRD's text
	@python3 scripts/controls.py check

scan-waivers: ## The scan waivers are well formed, and the scan, image and infra rules hold on planted findings (the scans themselves: make scan, make images, make infra-check)
	@python3 scripts/scan.py --self-test
	@python3 scripts/images.py self-test
	@python3 scripts/infra.py self-test

check-tokens: ## Design tokens (Fyniq and Nemura) meet WCAG contrast in all four themes (QA-UI-03)
	@python3 $(DESIGN)/scripts/check_contrast.py
	@python3 $(DESIGN)/scripts/check_contrast.py --tokens docs/design-system/nemura/tokens.css

check-config: ## Release configuration is valid and VERSION matches the manifest
	@python3 -m json.tool release-please-config.json > /dev/null
	@python3 -m json.tool .release-please-manifest.json > /dev/null
	@v=$$(tr -d '[:space:]' < VERSION); \
	 m=$$(python3 -c 'import json; print(json.load(open(".release-please-manifest.json"))["."])'); \
	 [[ "$$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$$ ]] || { echo "VERSION is not SemVer: $$v"; exit 1; }; \
	 [[ "$$v" == "$$m" ]] || { echo "VERSION ($$v) differs from .release-please-manifest.json ($$m)"; exit 1; }; \
	 echo "✔ release config valid; version $$v"
	@bash -n scripts/hooks/commit-msg scripts/check-commits.sh && echo "✔ hook scripts parse"

kb-index: ## Regenerate kb/index.md from the knowledge-base pages (AI-52)
	@python3 scripts/kb.py index

kb-lint: ## Lint the compiled knowledge base: stale sources, citations, grounded numbers, index (AI-52/53)
	@python3 scripts/kb.py lint

kb-stale: ## List knowledge-base pages whose sources changed, and sources no page covers
	@python3 scripts/kb.py stale

spec-check: ## OpenSpec specs are valid (strict) and match the PRD: coverage and drift (fyniq-openspec)
	@$(OPENSPEC) validate --specs --strict --no-interactive
	@python3 scripts/prdspec.py coverage --summary
	@python3 scripts/prdspec.py stale --summary

review-check: ## The review register names real requirements and screens, and no excluded page (FR-UX-41, QA-UI-09)
	@python3 scripts/review_status.py check

review-status: ## Areas awaiting second review and the pages that show the "Needs additional verification" banner
	@python3 scripts/review_status.py list

roadmap: ## Regenerate the P1–P3 plan's change tables, coverage and counts from the specs and the screen inventory
	@python3 scripts/roadmap.py write

roadmap-check: ## Every requirement and screen after P0 is placed in exactly one planned change, and the plan's tables are current
	@python3 scripts/roadmap.py check

spec-map: ## Show how each PRD's anchors map to OpenSpec capabilities
	@python3 scripts/prdspec.py map

spec-stale: ## List PRD rows changed since their specs were converted
	@python3 scripts/prdspec.py stale

design-kit: ## Regenerate the screen kit stylesheet from tokens.css and kit/ui.css
	@python3 $(SCREENS)/build.py kit

design-check: ## Lint the screen inventory and fragments for the Claude Design canvases
	@python3 $(SCREENS)/build.py check

design-status: ## Screens designed per canvas
	@python3 $(SCREENS)/build.py status

hooks: ## Install the git hooks (Conventional Commit check on every commit)
	@git config core.hooksPath scripts/hooks && echo "✔ hooks installed from scripts/hooks"

lint-commits: ## Check commit messages in RANGE (default: origin/main..HEAD)
	@scripts/check-commits.sh "$${RANGE:-origin/main..HEAD}"

deps-up: ## Start local dependencies: PostgreSQL 18, Mailpit, fake GCS
	@$(COMPOSE) up -d --wait
	@echo "PostgreSQL  postgres://fyniq:fyniq-local-only@localhost:$${FYNIQ_PG_PORT:-55432}/fyniq"
	@echo "Mailpit     http://localhost:$${FYNIQ_MAIL_UI_PORT:-8025}   (SMTP localhost:$${FYNIQ_SMTP_PORT:-1025})"
	@echo "Fake GCS    http://localhost:$${FYNIQ_GCS_PORT:-4443}/storage/v1/b"

deps-down: ## Stop local dependencies (keeps the database volume)
	@$(COMPOSE) down

deps-logs: ## Follow the logs of the local dependencies
	@$(COMPOSE) logs -f

prototype: ## Where to open the UI prototype
	@echo "file://$(CURDIR)/docs/design/prototype/index.html   (add ?mode=expert&theme=dark&contrast=more)"
