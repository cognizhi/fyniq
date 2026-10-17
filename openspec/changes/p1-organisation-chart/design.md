# Design

## Context

P0 built the database backstop:
- tenancy, row-level security, insert-only journals, the ledger revision and the balances cache;
- a minimal `fyniq.account`;
- the storage port with its PostgreSQL adapter (ADR-D1, ADR-T8);
- the API conventions (problem details, `Idempotency-Key`, `ETag` / `If-Match`, cursor lists) from
  `p0-api-conventions-authz`.

The sample company in `internal/seed` already carries a chart for `make seed-load`.

## Goals / Non-Goals

**Goals:**
- Every later module finds its accounts, dimensions, contacts, items and settings through the
  storage port, with the database refusing what the domain must never allow.

**Non-Goals:**
- Balances by contact beyond what P0's ledger holds. FR-CT-02 fills in as sales and purchases land.
- The payee name check of FR-CT-03, which waits for a bank API (P2).

## Decisions

- **Settings as typed rows, effective-dated.**
  - Each setting family is its own table, with validity periods. PostgreSQL 18 temporal keys,
    `WITHOUT OVERLAPS`, enforce that no two versions overlap. This is the "effective-dated reference
    data" requirement.
  - FR-ORG-08's recommended values ship as data. Each carries its reason and guide link, and
    resetting to the recommended value writes a new version.
  - Alternative considered: one JSON settings document. Rejected because its history and its
    constraints could not be enforced by the database.
- **System accounts by role, not by code.**
  - `fyniq.account.role` (000014, today only `share-capital`) gains the FR-COA-03 system roles by
    extending its CHECK, as that migration planned. Triggers refuse deleting or retyping a system
    account, and manual posts to an account the engine owns (FR-COA-03), as they already do for
    share capital (INV-13).
  - An account with postings can only be deactivated or merged (FR-COA-04). The merge posts
    reclassification journals through `post_entry()`, so history is preserved.
- **Dimensions on lines.**
  - `journal_line` gains a dimension set: one row per distinct combination, referenced by a key.
  - The balances cache is keyed by account × period × dimension set × currency, as §11.4 already
    plans.
  - "Required per account" is checked in the domain, and the database backstops it on posting.
- **Vendor bank details are a versioned child record.**
  - A change creates a pending version, freezes payments to the vendor, notifies the founders through
    the outbox, and waits for the approval engine (SOD-1, FM-1). Only an approved version becomes
    current.
  - Bank numbers are masked in logs and responses (SEC-15).
- **Gap-free sequences.** A per-organisation sequence row is locked inside the posting transaction,
  in the lock order of ENG-47, so numbers never skip or repeat.
- **Logo upload.**
  - The upload goes through the blob-store port (local directory or fake GCS). The SVG is reduced to
    an allow-listed subset on the server; scripts, external references and event attributes are
    refused (G4-099).
  - Issued documents keep the logo version they were issued with.
- **Ports and adapters (ADR-T8):** storage (PostgreSQL), blob store (local and GCS), queue (outbox).
  No new port.
- **Audit events (AUD-01):** every create, update, deactivation, merge and setting version, written
  in the same transaction. Bank-detail changes record before and after values masked.

## Risks / Trade-offs

- [The dimension-set key changes the balances cache's key, which P0's INV-08 rebuild and attack tests
  rely on] → Write the migration as expand and contract, extend the rebuild and the attack tests in
  the same change, and have the oracle's half compare balances per dimension set (QA-ENG-02).
- [The accountant may change the chart's attributes] → Pre-load the chart from data, never from code,
  so a correction is a data change with a test.

## Migration Plan

Forward-only goose migrations, linted by squawk with lock and statement timeouts. Existing sample
data is backfilled with the default dimension set. There is no rollback beyond a new forward
migration.
