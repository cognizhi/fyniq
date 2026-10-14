# Proposal

## Why

The repository has the spec, the design system and the release tooling but no application tree. Every
other P0 change needs the monorepo layout, the `make` contract and local mode in place first, so that
`make test` runs every gate offline from the first line of code (PRD-001 §12.8, ADR-T4, golden rule 9).

## What Changes

- The monorepo layout of PRD-001 §12.4: one Go module with `cmd/<service>/` for `api`, `worker`,
  `executor`, `mcp`, `migrate` and the sample-company `seed` tool; `internal/` with `domain/`, `ports/`
  and `adapters/{local,gcp}/`; a pnpm workspace with an `apps/web` shell; `contracts/openapi/`,
  `db/migrations/`, `oracle/`, `deploy/docker/` and the existing `deploy/local/` Compose stack.
- A pinned toolchain in `mise.toml` and a dev container in `.devcontainer/` (DEV-03).
- The local-mode command contract in the Makefile, each with a `## description`: `setup`, `dev`, `up`,
  `down`, `seed`, `reset`, `test`, `test-e2e`, `evals`, `images`, `ci` (DEV-01, DEV-02, NFR-MAINT-01).
- An injectable clock with a "business date" override outside production (DEV-05).
- Build tags: local adapters compile only with the `local` tag, and a production binary refuses to
  start with a local adapter or with `FYNIQ_ENV` other than `staging` or `production` (DEV-07, SEC-16).
- A first cut of the one sample-company generator, producing synthetic data only (DEV-04, DEV-08).
- `/healthz`, `/readyz` and `/version` on every service (NFR-IMG-06).
- An architecture test that fails when a cloud SDK is imported outside `internal/adapters/<provider>/`
  (NFR-PORT-03).
- AGENTS.md: the commands table moves these targets from "Arrives in P0" to "Today".

## Capabilities

### New Capabilities

None. Every requirement this change builds is already approved in the main specs.

### Modified Capabilities

None (`skip_specs: true`). It builds `platform/local-mode` (DEV-01…05, DEV-07, DEV-08, NFR-MAINT-01,
NFR-DEVX-01…03), `platform/portability-and-cost` (NFR-PORT-03), `platform/container-images` (NFR-IMG-06)
and `security/data-protection` (SEC-16).

## Impact

- New top-level directories (`cmd/`, `internal/`, `apps/`, `contracts/`, `db/`, `oracle/`), `mise.toml`,
  `.devcontainer/`, the Makefile and AGENTS.md.
- No screens: the `apps/web` shell is a placeholder until the P1 screens are built from
  `docs/design/screens/`.
- Dependencies: Go 1.27, Node 24 LTS, pnpm, Python (for `oracle/`), as pinned in `mise.toml`.
