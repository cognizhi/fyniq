# Design

## Context

Nemura's outbox, API clients (single Owner with a fresh passkey), passkeys (`NEMURA_RP_ID`, the shared registrable domain) and approvals exist, and
the archtest forbids Nemura importing Fyniq's code: both sides implement the contract and each tests the same fixtures. Nemura has no link, no payroll
engine yet (`p1-nemura-payroll`, another session's), so payroll events are built and tested from contract fixtures.

## Goals / Non-Goals

**Goals:** the link record and lifecycle on Nemura's side; the signed outbox events and inbox; the approval envelope for a run; the segregation rules across
both apps; one sign-in and the switch; health and restore receipts.
**Non-Goals:** the payroll calculation, payslips, the CPF file (`p1-nemura-payroll`, `p2-payroll-outputs`); a standalone Nemura's journal file (NM-ARC-01);
the people assistant (`p1-nemura-assistant-external`).

## Decisions

- **D1. The shared design is `p1-payroll-link`'s D1 to D12.** The envelope, its canonical bytes, the two credentials, the passkey verification source
  (D3) and the second approver (D11) are decided once there; this change implements Nemura's half and tests the same fixtures.
- **D2. The link record is Nemura's too.** `fyniq_link` (state, epoch, Fyniq organisation, Fyniq endpoint, the link key reference, the client), moved
  through proposed, active, suspended, unlinking and unlinked by the Owner and a second approver; the epoch increases on each re-link and every event
  carries it. Unlinking waits for unsettled work (runs approved and not posted, unknown payment outcomes, holds moving to Fyniq, events waiting).
- **D3. The link key and its custody.** An Ed25519 key pair made when the link is proposed; the private half is held by Nemura's worker role only (sealed
  at the application level, never in logs or the API), the public half is given to Fyniq in the approved link. Rotation creates a new key and a new epoch.
- **D4. The outbox gives delivery, the inbox the outcome (ADR-T7).** Events leave through Nemura's outbox with the envelope signed at send time; the
  worker's HTTP calls go through `internal/egress`; Fyniq's answers (`payroll.run.posted`, `payroll.payments.status`) arrive as signed webhooks into
  Nemura's inbox with the same rules (dedupe by event ID, older versions ignored, tombstones, gap repair).
- **D5. Priority events go ahead of ordinary sync.** `people.tax_hold.changed` and `people.bank_details.changed` use a priority queue so a hold reaches
  Fyniq before the next batch release; the near-pay-day alert fires within the hour (NM-INT-04).
- **D6. Sessions.** Each app keeps its own audience-bound session made from the shared sign-in; deactivation or sign-out in one writes a revocation the other
  reads (a signed `identity.session.revoked` event and a poll fallback) so both end within 60 seconds. People are matched by issuer, subject and tenant,
  never by email; linking a tenant that ran standalone maps each person explicitly and grants no Fyniq role.
- **D7. The switch.** Fyniq's header shows Finance | People when the person's rights span both apps (computed from Fyniq's role set and Nemura's role
  answer, never the other app's token); it keeps the last page in each app; a person with Nemura rights only lands on People home.
- **D8. Restore.** Both apps keep a per-run receipt (submission digest, posting reference, instruction IDs and outcomes) and a reconciliation checkpoint
  (the highest event sequence each side has acknowledged); after a restore, work after the checkpoint is held as unknown and reconciled against the other
  app's receipts before anything is sent or paid.

## Risks / Trade-offs

- Cross-app tests need two databases and two services: the stack tests use each app's own Testcontainers database and a loopback pair, never the cloud.
- A session-revocation window of 60 seconds is a poll interval plus the event; the test fixes the clock, the cloud's latency is `[live-verify]`.

## Open questions

None blocking; D3 and D11 of `p1-payroll-link` await the owner.
