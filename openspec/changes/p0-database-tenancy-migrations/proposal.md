# Proposal

## Why

The database is the backstop (golden rule 2): balanced entries, immutable posted history, locked periods
and tenant isolation must hold even when application code is wrong. P0 builds that backstop, and the
storage port in front of it, before any feature writes a row (PRD-001 §11.3, §11.4, §10.7, ADR-D1).

## What Changes

- goose SQL migrations in `db/migrations/NNNNNN_name.sql`, applied by the `migrate` binary as a
  run-to-completion job under its own role; squawk lints every migration in CI.
- Database roles: `fyniq_migrator` (DDL only), one role per service (`api`, `worker`, `executor`) with
  DML grants on what it needs, a read-only role, and no human role with standing write access (SEC-03).
- Tenancy: `organisation_id` on every tenant table, set per transaction with
  `set_config('app.org_id', …, true)`, `FORCE ROW LEVEL SECURITY` and a policy on every table; a
  catalogue test fails on any table without them (§11.4, INV-06, CTL-IAM-05).
- The ledger's core tables: organisations, accounts (minimal), periods, journal entries and lines,
  the ledger revision counter (ENG-46), the posting identity and idempotency keys (§11.4), and the
  materialised balances cache with its rebuild (INV-08).
- Constraints and triggers for INV-01 (balanced entries), INV-02 (posted entries immutable; corrections
  by reversal), INV-03 (no posting into a closed period), INV-04 (active leaf accounts of the same
  organisation), INV-05 (amounts at minor-unit scale), INV-08 (materialised balances equal recomputation)
  and INV-09 (trial balance nets to zero). Money columns are `NUMERIC(20,4)`, and FX rates
  `NUMERIC(20,10)` (open question 30).
- The concurrency protocol: READ COMMITTED with row locks taken in one fixed order (ENG-47).
- The storage port and unit of work, with pgx v5 and sqlc used only inside the PostgreSQL adapter
  (NFR-PORT-01, NFR-PORT-02, NFR-PORT-07), and its conformance suite (NFR-PORT-04).
- Attack tests that bypass the application and attempt every forbidden write, and race tests on
  independent connections (QA-ENG-05).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds `data/data-model` (§11.4), `ledger/invariants` (INV-01…06, INV-08,
INV-09), `ledger/posting-engine` (ENG-46, ENG-47), `platform/portability-and-cost` (NFR-PORT-01, 02,
04, 07), `security/identity-and-access` (SEC-02, database half; SEC-03) and `quality/calculation-correctness`
(QA-ENG-05). INV-07, INV-10, INV-11 and INV-13 arrive with their modules in P1; INV-12 comes with
p0-audit-chain.

## Impact

- `db/migrations/`, `internal/adapters/postgres/`, `internal/ports/storage`, `cmd/migrate`.
- **Second human reviewer required** (AGENTS.md: tenancy and the ledger).
