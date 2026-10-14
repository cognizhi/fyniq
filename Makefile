# Fyniq: the single entry point for local work and CI (PRD-001 §12.8).
#
# Available today: the targets below.
# Arrives in P0 (command contract, ADR-T4): setup, dev, up, down, seed, reset, test,
# test-e2e, evals, images, ci. See .claude/skills/fyniq-local-dev.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

COMPOSE := docker compose -f deploy/local/compose.yaml
DESIGN := docs/design-system/fyniq
SCREENS := docs/design/screens
# OpenSpec CLI: the installed one, else the pinned version through npx (CI)
OPENSPEC ?= $(shell command -v openspec >/dev/null 2>&1 && echo openspec || echo npx -y @fission-ai/openspec@1.14.1)
export OPENSPEC_TELEMETRY := 0

.PHONY: help check check-tokens check-config kb-index kb-lint kb-stale spec-check spec-map spec-stale design-kit design-check design-status hooks lint-commits deps-up deps-down deps-logs prototype

help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z-]*:.*## / {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "  Arriving in P0: setup dev up down seed reset test test-e2e evals images ci"

check: check-tokens check-config kb-lint spec-check design-check review-check ## Run every check available today (CI runs this)

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
