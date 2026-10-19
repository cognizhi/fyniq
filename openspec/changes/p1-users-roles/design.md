# Design

## Context

P0 (`p0-identity-oauth-first-run`, `p0-api-conventions-authz`) built:
- the `SignIn` port, with a local sign-in page and an Identity Platform adapter tested against its
  fake;
- the Zoho issuer check;
- first-run setup and Super admin recovery;
- sessions in an HTTP-only cookie;
- Hydra with opaque tokens and introspection;
- one policy table that checks role and attribute in the API, with row-level security in the database
  (SEC-02);
- the authorisation-matrix test skeleton (QA-SEC-03).

`fyniq.role_assignment` holds the roles.

## Goals / Non-Goals

**Goals:**
- Appendix D is the single source of rights. The API, the database scope predicates, the UI's
  workspaces (FR-UX-40) and the AI's read scope all derive from it.

**Non-Goals:**
- SSO and SCIM (P3, Enterprise mode).
- The team modes' rules FM-1…9 (`p1-approval-engine`).
- MCP connections (wave 6).

## Decisions

- **Appendix D as data.**
  - The matrix is transcribed once into `internal/authz/matrix.csv`, with the role, area, right
    letters (C A P R V M —) and conditions.
  - The policy table is generated from it, and a test compares the generated table with the
    PRD's Appendix D rows, so a PRD change that is not carried over fails `make test`.
  - Alternative considered: hand-written Go policies. Rejected because they would drift from the
    PRD.
- **Segregation of duties checked at action time.**
  - SOD-1…10 are rules over the record's history: who prepared it, who changed the bank details,
    who recorded the lines. They are evaluated in the domain when the action runs, with the audit
    trail as the evidence.
  - The team mode decides whether a conflict blocks (Standard and Enterprise) or needs FM controls
    (Founder mode, `p1-approval-engine`).
- **Passkeys in the API.**
  - WebAuthn registration and assertion with a server-side challenge store: single use, within the
    ceremony timeout.
  - Assertions are kept for later re-verification (FR-APR-03's evidence).
  - The library is chosen at build time from maintained Go WebAuthn libraries, pinned and at least 2
    weeks old.
- **Recovery is a state machine.**
  - Requested, then the first proof, the second proof, cooling-off, restored.
  - Each step is audited, and every Owner and Super admin is notified. A recovery revokes all
    sessions and authenticators.
  - The two proofs must come from independent channels (SEC-35); email plus support is never two.
- **Role change within 60 seconds.** Session and token introspection re-read the role version, cached
  for at most 60 seconds, as P0's AI-38 test already does for tokens.
- **Ports and adapters (ADR-T8):** `SignIn` (local, Identity Platform), `OAuth` (Hydra), storage and
  queue (sign-in alerts and notices through the outbox).
- **Audit events (AUD-01):**
  - invitations, roles and scope changes, MFA enrolment and removal;
  - session revocation, recovery steps, Owner changes and client changes.
  - None carries a secret, a TOTP seed or an assertion's private material (SEC-15).

## Risks / Trade-offs

- [The matrix has 37 areas and many conditions, so a transcription error grants too much] → The
  generated table is compared with the PRD rows, every cell gets an allowed or denied test, and an
  unknown area defaults to deny.
- [Passkey support differs by browser and platform] → TOTP stays available as the second factor;
  step-up accepts either, and the WCAG accessible-authentication rule (NFR-A11Y-05) is tested.
