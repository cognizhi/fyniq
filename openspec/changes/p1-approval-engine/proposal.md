# Proposal

## Why

Bills, payments, journals above the threshold, vendor bank changes, pack activations, policy changes
and, from wave 6, AI proposals all wait for a named person's approval. Cognizhi runs in Founder mode
at go-live: its founders may hold conflicting roles only under the compensating controls FM-1…9.
These need one engine that signs exactly what was approved and refuses to execute anything else
(§6.4, §6.5). Wave 2's documents need it before they can post.

## What Changes

- **Approval policies per object type (FR-APR-01):** conditions and ordered steps with a quorum.
- **The approval view (FR-APR-02):** the document, its ledger impact, the affected balances and
  checks, and a diff against the previous version.
- **Signed approval records (FR-APR-03):** the approver, the time, the authentication method and the
  SHA-256 hash of the approval envelope. A payload with a different hash never executes; a passkey or
  TOTP assertion is kept for re-verification (G4-056).
- **Step-up for high-risk approvals (FR-APR-04).**
- **Expiry and invalidation (FR-APR-05):** 7 days by default, and invalid when the record or any
  dependency version in the envelope changes (G4-055).
- **Approval-gated, versioned policy changes (FR-APR-09):** SOD-8, and a pending approval keeps its
  policy version (G4-057).
- **The approval envelope (FR-APR-10):** canonical JSON (RFC 8785), rebuilt and compared in the
  executing transaction. AI proposals use the same envelope later.
- **The team modes:** Founder, Standard and Enterprise, with dual-controlled switching.
- **Founder mode's controls FM-1…9:**
  - FM-1: the two-person rule;
  - FM-2: self-approval below S$2,000 by default, flagged and queued for review;
  - FM-3: the weekly review;
  - FM-4: the monthly close attestation;
  - FM-5: reconciliation balances never typed;
  - FM-6: AI anomaly review as advice, never approval (it arrives with wave 6);
  - FM-7: the accountant's quarterly review record;
  - FM-8: break-glass for a single available founder;
  - FM-9: a founder never approves the settlement of their own balance.
- **The control check (FR-CTL-04):** period lock, approvals, segregation of duties and team mode
  enforced together.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `access/approvals`: FR-APR-01…05, 09 and 10;
- `access/team-modes`: FM-1…9 and the four mode requirements;
- `controls/checks`: FR-CTL-04.

FM-4, FM-5 and FM-7 depend on the close and reconciliation of waves 2 and 3. Their rule and record
land here, and those changes call them.

## Impact

- **Code:** a new `internal/domain/approval` package, migrations for policies, steps, envelopes,
  records, break-glass and review records, the API and the screens. `p1-organisation-chart`,
  `p1-statutory-packs` and `p1-users-roles` switch their refusals to this engine.
- **Screens:** accountant-review, approval-confirmation, approval-policies, bank-details-change,
  payment-batch, personal-approvals and team-mode.
- **Security boundary:** money movement and approvals. The reviewer is the co-founder (#6).
- **Compliance:** the Electronic Transactions Act conditions on approvals (§14.2) are met by the
  stored record and the void-on-edit rule.
