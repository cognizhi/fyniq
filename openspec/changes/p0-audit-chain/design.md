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
