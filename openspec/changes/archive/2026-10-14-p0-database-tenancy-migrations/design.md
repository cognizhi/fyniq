# Design

## Context

ADR-D1 (accepted, rev 0.21): PostgreSQL 18 on Cloud SQL; locally, PostgreSQL 18 on port 55432
(`make deps-up`). ADR-T8: the domain sees only the storage port. Golden rules 2 and 8.

## Goals / Non-Goals

**Goals:**
- Every invariant P0 owns is enforced by the database itself and proved by an attack test that bypasses
  the application.
- Tenant isolation holds even if the API forgets a filter.

**Non-Goals:**
- Posting logic (p0-money-posting-engine) and the audit chain (p0-audit-chain).
- The P1 subledgers and the invariants that need them (INV-07, INV-10, INV-11, INV-13).

## Decisions

- **Transaction-scoped tenant context.** The unit of work opens a transaction and runs
  `set_config('app.org_id', $1, true)` first. Policies read `current_setting('app.org_id', true)`, so a
  missing setting matches no rows rather than all of them. Alternative: a session setting, rejected
  because pooled connections would leak it between tenants.
- **FORCE ROW LEVEL SECURITY everywhere.** Table owners are bound by the policies too. Only
  `fyniq_migrator` owns tables, and no service connects as it.
- **Immutability by trigger, not by grant alone.** Grants stop the service roles from updating or
  deleting posted rows; a trigger also rejects the change from any role, so a mistaken grant cannot
  open history.
- **Balanced entries checked at commit.** A deferred constraint trigger sums each entry's lines per
  currency at commit (INV-01), so a multi-statement insert can build an entry inside its transaction.
- **Fixed lock order (ENG-47).** The order is organisation ledger-revision row, then period, then
  accounts by ID. Every write path goes through the unit of work, which takes the locks in that order.
- **Audit events.** The tables this change creates are audited by p0-audit-chain, which lands before
  any service writes to them outside tests.
- **Ports and adapters.** `StoragePort` and `UnitOfWork` in `internal/ports/storage`; the PostgreSQL
  adapter in `internal/adapters/postgres` (pgx v5, sqlc); a conformance suite that any other adapter
  must pass (NFR-PORT-04).

## Risks / Trade-offs

- [Triggers slow bulk imports] → measure with production-shaped data; imports use the same path and
  the performance budgets in PRD-001 §15 decide whether to batch.
- [An RLS policy bug leaks across tenants] → the catalogue test plus an attack test per table reads as
  organisation B after writing as organisation A, and must see nothing.

## Migration Plan

Each migration is expand-and-contract and never edited once merged (§12.10). `migrate` runs as a job
before a deploy; rollback is a new forward migration.

## Implementation notes (14 Oct 2026)

What the build settled, and where it differs from the plan above. `db/README.md` is the reference
for roles, rules and error codes.

- **Roles.** Migration 000001 creates the service roles as NOLOGIN group roles: `fyniq_api`,
  `fyniq_worker`, `fyniq_executor`, `fyniq_readonly` and also `fyniq_ai`, because QA-ENG-05 attacks
  "write as the AI role" and AI-P2 gives the AI runtime its own role (it holds no grant). The
  environment creates `fyniq_migrator` before the first run: locally `migrate up` in the local build
  does it with the container's superuser and then lets each role log in with the local-only password;
  in the cloud p0-infra-opentofu does it with IAM database authentication. `fyniq_migrator` has
  CREATEROLE, which PostgreSQL 16+ limits: it cannot grant SUPERUSER or BYPASSRLS, and it administers
  the roles without being able to act as them.
- **Schemas.** `fyniq` for the application, `fyniq_ext` for pgcrypto and btree_gist, and
  `fyniq_migrations` for goose's table; services can read none of the last two.
- **The port** is `storage.Store` (opens units of work) and `storage.UnitOfWork` in
  `internal/ports/storage`. Amounts cross it as exact decimal text (`storage.Decimal`);
  p0-money-posting-engine builds them from `Money`.
- **The tenant** is read through `fyniq.current_org()`, which turns the empty string a pooled
  connection keeps after a transaction-scoped setting into NULL, so it matches no row instead of
  failing the cast.
- **Close and reopen.** `period_guard` records the ledger revision at the close under the ledger lock,
  so a writer that skips the unit of work cannot record a wrong one; the API role has no grant on that
  column. The unit of work's transactions set `lock_timeout` to 5 s (ENG-47); retrying deadlocks with
  the same Idempotency-Key belongs to the API (p0-api-conventions-authz).
- **INV-05.** Base-only lines are refused outright until FX revaluation (FR-GL-04, P1) adds its source
  and the separate trigger the invariant describes.
- **Deferred to the modules that need them:** dimension sets in the balances key (FR-DIM); ledger
  revisions on master-data versions such as `account_version` (ENG-46, P1); an organisation directory
  the nightly verification can iterate (p0-outbox-background-work).
- **QA-ENG-05 races.** Built: a posting against a close of the same period, a reopen against a
  posting, and a report "as at revision N" against a back-dated posting (read from the lines; the
  balance service of p0-money-posting-engine reuses the rule). Pending: two allocations to one invoice,
  which needs the P1 receivables allocations.
- **Where the suites run.** The conformance, catalogue, attack and race suites run with
  `go test -tags local` (part of `make test`) against `postgres:18.6-alpine` in Testcontainers. They
  fail, never skip, when Docker is missing.
- **Versions.** pgx 5.11.0, goose 3.28.0 (3.29.0 was three days old, so the goose CLI pin in
  `mise.toml` moved back to match), testcontainers-go 0.44.0, sqlc 1.31.1, squawk 2.66.0.
- **Task 4.3 is open.** On 14 Oct the founders disabled the `ci` workflow (`gh workflow disable ci`)
  and asked for the gates to run locally until it returns. The suites run in `make test`, which
  fails when any of them fails, and the last CI run of this change passed every job. "Blocks merge
  in CI" is proved once `ci` is enabled again and `Go, web and checks (make test)` is a required check.
  (Done the same day: the repository went public, `ci` was enabled again, and
  `Go, web and checks (make test)` became a required check on `main` beside the commit and design
  checks, so a failing attack or race test blocks the merge.)
