# Design

## Context

The unit of work (p0-database-tenancy-migrations) owns every write. ADR-T7 runs long work as jobs;
ADR-T8 keeps the bucket and the signing key behind ports.

## Goals / Non-Goals

**Goals:**
- An audit event cannot be skipped by any write path, and the chain cannot be altered without detection.

**Non-Goals:**
- The audit trail screens and "Verify integrity" for users (P1).

## Decisions

- **Written by the unit of work, checked by the database.** The unit of work appends the audit event
  before commit. A deferred trigger on each audited table rejects a commit that changed rows without a
  matching audit event in the same transaction, so a write path that forgets cannot commit.
- **Hash chain per organisation.** `hash = SHA-256(canonical JSON of the event without hash ‖
  prev_hash)`. The chain head row is locked in the ENG-47 order, so chains never fork.
- **Anchors.** Every hour a job signs `{organisation_id, head_hash, event_count, at}` with a KMS key
  (`Signer` port; local adapter: an Ed25519 key in the dev container) and writes it to the WORM bucket
  (`BlobStore` port with a retention lock).
- **Nightly verification.** A job recomputes each chain from the first event and compares it with every
  anchor. A mismatch raises an alert and a ✖ check (INV-12).
- **Masking.** Fields tagged sensitive in the schema registry (bank numbers, NRIC, tokens) are masked in
  `changes` and in logs (SEC-15).
- **Audit events emitted.** `audit.anchor.written` and `audit.verification.completed` from the jobs
  themselves.

## Risks / Trade-offs

- [Contention on the chain head under concurrent writes] → one head per organisation, and organisations
  are small. Measure with the race tests, and batch per transaction if needed.
- [The KMS key is lost or rotated] → anchors record the key version, and rotation keeps old versions for
  verification.

## Implementation notes (15 Oct 2026)

- **Hash order follows the PRD.** §11.5 says `hash = SHA-256(prev_hash ‖ canonical_event_json)`; the
  decision above has the operands the other way round. The build follows the PRD: the 32-byte
  `prev_hash` (zero bytes for the first event) followed by the body's UTF-8 bytes, exactly as
  stored. `contracts/audit/README.md` specifies it for outside verifiers.
- **Who chains.** The unit of work builds each event (`internal/domain/audit`: canonical JSON,
  masking) and the database's `audit_event_append` trigger assigns `seq`, `prev_hash` and `hash`
  under the chain head's row lock. That lock serialises appends per organisation as §11.5's
  transaction-scoped advisory lock would. Values the database assigns stay out of the hashed body;
  `event_id` is a UUIDv7 made in Go and checked against the column.
- **Written last.** Events are buffered on the unit of work and written as the last statements before
  commit, so the chain head is always the last lock (documents, ledger revision, periods, then audit;
  ENG-47), and the deferred `require_audit` triggers (AUD-08) see them. The triggers are named so the
  invariant triggers' more specific errors fire first.
- **Every repository write is audited by the adapter**, with before and after values; who acted, on
  whose behalf, under which approvals and why come from the request context
  (`storage.WithAudit`, `WithReason`), and the store's database role stands in for a service with no
  user. `Audit().Record` adds events that are not row changes (anchors, verification runs, imports).
- **Masking (SEC-15)** applies to changes and reasons by field name (secrets and tokens removed;
  NRIC/FIN and bank or card numbers keep their last four characters) and by shape in any text; the
  same rules mask the new structured logger (`internal/logging`). AUD-02's encrypted values are P1.
- **Anchors** are signed through a new `Signer` port (local adapter: Ed25519 key in `.data/keys/`) and
  written write-once (`BlobStore.Create`, which also gained `List`). The Cloud KMS and Cloud Storage
  adapters, with Bucket Lock, come with p0-infra-opentofu, so task 2.1's staging part and task 2.3
  stay open. A chain whose last event is an anchor is not anchored again, so idle organisations do not
  grow their trails every hour.
- **Jobs.** `cmd/jobs audit-anchor` and `audit-verify` run every organisation, found through a new
  global `tenant_directory` (view `tenants`, the worker only). Verification records
  `audit.verification.completed` in each trail and exits non-zero on any failure, which is the alert
  until monitoring arrives (p0-infra-opentofu); the ✖ check joins the checks catalogue in P1, so task
  2.2 stays open for it. Hourly and nightly scheduling comes with p0-outbox-background-work.
- **Tamper tests** run as the database superuser with triggers off (`session_replication_role =
  replica`): an edited body is caught by the chain, and a chain rewritten consistently from the
  change onwards is caught by the anchor written before it.
