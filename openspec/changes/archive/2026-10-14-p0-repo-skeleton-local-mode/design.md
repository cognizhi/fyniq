# Design

## Context

ADR-T1 (accepted, rev 0.21) fixes the stack: Go for the services, React for the web app, Python only for
the oracle. ADR-T4 (local mode) and ADR-T8 (ports and adapters) are accepted. `deploy/local/` already
runs PostgreSQL 18 on port 55432, Mailpit and fake GCS (`make deps-up`).

## Goals / Non-Goals

**Goals:**
- A clean machine with Docker and `mise` reaches a running stack with `make setup && make dev` in the
  DEV-01 budget, with no cloud credentials.
- The layout and build tags make the wrong thing hard: no cloud SDK in the domain, no local adapter in a
  release build.

**Non-Goals:**
- Business features, the database schema (p0-database-tenancy-migrations), images and CI
  (p0-ci-images-release).

## Decisions

- **One Go module, one `cmd/` per service.** Shared code lives in `internal/`, so nothing outside the
  repository imports it. Alternative: a module per service, rejected for version skew in a 2–3 person
  team.
- **Ports in `internal/ports`, adapters in `internal/adapters/<provider>/` (ADR-T8).** This change adds
  the `Clock`, `Secrets`, `BlobStore` and `Queue` ports with local adapters. Their GCP adapters come with
  p0-infra-opentofu and p0-outbox-background-work.
- **Build tags.** `//go:build local` on every local adapter. `internal/runtime` checks `FYNIQ_ENV` at
  start and exits non-zero in a release build with a local adapter linked (DEV-07). A unit test builds
  both flavours and asserts the refusal.
- **Architecture test.** A Go test runs `go list -deps` for every package outside
  `internal/adapters/` and fails on `cloud.google.com/`, `github.com/aws/` or `github.com/Azure/`
  imports (NFR-PORT-03).
- **Hot reload.** `watchexec` restarts a Go service on change; Vite's hot module reload serves the web
  shell (NFR-DEVX-03). Alternative: `air`, equivalent; `watchexec` is one binary already in `mise`.
- **Sample company.** `cmd/seed` builds a deterministic synthetic company from a seed number, so the
  same data feeds local mode, golden fixtures and later the sandbox (DEV-04). It reads names and numbers
  only from `docs/design/screens/sample-data.json`, which is already synthetic.
- **Audit events.** None: this change saves no state.

## Risks / Trade-offs

- [The DEV-01 and NFR-DEVX budgets are tight on Docker Desktop and WSL] → measure them in CI on every
  merge and record the times; cache Go and pnpm downloads in the dev container image.
- [Placeholder targets hide missing gates] → `test-e2e` runs a real Playwright smoke on the shell, and
  `evals` runs the harness in replay mode with an empty suite, printing that P1 adds the cases.
