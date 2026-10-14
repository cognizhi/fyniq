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
