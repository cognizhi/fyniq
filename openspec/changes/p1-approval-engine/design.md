# Design

## Context

- Passkeys and TOTP step-up come from `p1-users-roles`.
- Records are versioned (FR-INT-09).
- The outbox carries notices.
- The posting engine posts through `post_entry()` with a ledger revision.
- P0's manual journals post without approval, as the P0 rules allowed (ENG-24).

## Goals / Non-Goals

**Goals:**
- Nothing that needs approval executes unless the envelope rebuilt inside the executing transaction
  is byte-for-byte the one a named person signed.

**Non-Goals:**
- The AI proposal catalogue (wave 6): it reuses the envelope.
- Enterprise mode's SSO and SCIM (P3).

## Decisions

- **The envelope is the unit of approval.**
  - It holds the object type and ID, the record version, every dependency version, the policy
    version, the ledger impact and the amount.
  - It is serialised as RFC 8785 canonical JSON and hashed with SHA-256.
  - At execution, the same builder runs inside the executing transaction, with rows locked in the
    ENG-47 order. Any difference blocks execution with a stable error code.
  - Alternative considered: storing and replaying the approved payload. Rejected because it would
    execute stale data.
- **The database refuses an approval record without a hash and an MFA claim** (§14.2,
  electronic-signature conditions). Editing the record after approval voids it by a trigger on the
  version, not by application code.
- **The quorum signs one envelope.** Each step's approvals reference the same hash. A policy change
  restarts pending approvals (FR-APR-10), and a pending approval keeps its policy version (G4-057).
- **Founder mode as policy, not as a bypass.**
  - FM-1…9 are rules in the same engine, evaluated with the team mode.
  - The self-approval threshold is an FR-ORG-08 setting, S$2,000 by default.
  - The beneficiary is excluded automatically (FM-9). When every available approver benefits, the
    action waits.
  - Break-glass (FM-8) is a time-boxed grant: 24 hours, a reason, step-up, the allowed actions only
    and a total cap. Payees with a recent bank change are excluded. It cannot extend itself, it
    notifies everyone and a banner shows, and it needs a post-review.
- **Expiry uses the app clock.** Expiry is set from the API's clock, as in the #52 fix, so local mode's
  business date (DEV-05) and the database agree.
- **Ports and adapters (ADR-T8):** storage, queue (notices, weekly review tasks), clock. No new port.
- **Audit events (AUD-01):**
  - approval requested, given, refused, expired or voided;
  - policy proposed or approved;
  - mode switched;
  - break-glass started, ended or reviewed;
  - self-approval flagged;
  - weekly review done.

## Risks / Trade-offs

- [A dependency missing from the envelope lets a change slip through, as in the G4-055 bank-detail
  case] → Each object type declares its dependencies in one table; a test per type changes each
  dependency after approval and expects a block.
- [Founder mode's friction on a two-person team] → FM-2's threshold and the weekly review keep
  routine work moving, and the threshold is a setting with its reason (FR-ORG-08).
