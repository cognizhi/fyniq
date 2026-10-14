# Proposal

## Why

Every saved change must be audited in the same transaction, and the trail must be provably untampered
(golden rule 7). P0 builds the audit chain before features write data, and §17.3 exit item 3 requires it
to be verified nightly and anchored in staging (PRD-001 §11.5).

## What Changes

- The audit event schema of AUD-01, versioned and documented: `event_id`, `organisation_id`,
  `occurred_at`, `actor`, `on_behalf_of`, `originator`, `approval_ids[]`, `action`, `object`, `changes`
  (before and after), `reason`, `request`, `prev_hash` and `hash`.
- Audit events written by the unit of work in the same transaction as the change they record, with a
  per-organisation hash chain (INV-12).
- No setting, role or plan can switch the trail off or reduce it (AUD-08).
- An hourly signed anchor of each chain head, written to a retention-locked (WORM) bucket in staging and
  production, and to the fake GCS bucket locally (CTL-AUD-02).
- A nightly verification job that recomputes every chain and checks it against the anchors, for all
  organisations (AUD-03, P0 part). The user-facing "Verify integrity" screen is P1.
- Logs never contain secrets, tokens, full NRIC or bank numbers; audit `changes` mask those fields
  (SEC-15).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds `data/audit-trail` (AUD-01, AUD-03 P0 part, AUD-08),
`ledger/invariants` (INV-12) and `compliance/financial-controls` (CTL-AUD-02).

## Impact

- `internal/domain/audit`, the unit of work, a `cmd/worker` job for anchoring and verification, the
  `BlobStore` port (WORM bucket via p0-infra-opentofu), the KMS signing key.
- **Second human reviewer required** (AGENTS.md: the audit trail).
