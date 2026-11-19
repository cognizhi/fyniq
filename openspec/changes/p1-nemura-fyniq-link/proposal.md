# Proposal

## Why

Nemura is built to run standalone, and to link to Fyniq for the books (PRD-002 §16). Nothing yet connects them: Nemura has no link state, no signed events
to Fyniq, no second approver on its API clients, no Finance | People switch and no way for one sign-in to cover both apps. The pair `p1-payroll-link`
builds Fyniq's side; this change builds the cross-app contracts, Nemura's side of the events, the identity and session rules across both apps, and the
segregation a pay run needs when two applications and two databases are involved. Owner decisions (16 and 19 Nov 2026): payroll lives in Nemura; PAT and
API-client dual control arrives with this link (NM-ARC-04); one Go module; `people.<domain>`; Nemura's image rides the release train.

## What Changes

- **Linking** (NM-ARC-04, NM-INT-11): Nemura's side of the link record, states and epoch, Owner of both and a second approver; the second approver on
  Nemura's API clients from the day the link exists.
- **Events to Fyniq** (NM-INT-01, 05, 10): the outbox's signed envelopes for `people.person.upserted`, `people.bank_details.changed`,
  `people.tax_hold.changed`, `leave.taken.changed`, `payroll.run.approved` and `payroll.payments.ready`; the minimum data (never NRIC or FIN, medical,
  disciplinary or review data); the inbox for what Fyniq sends back (`payroll.run.posted`, `payroll.payments.status`, `people.department.changed`).
- **Runs and proof** (NM-INT-03, 06, 07, 08, 09): the approval envelope over a run's canonical figures, the approver's signed record, a refused run waiting
  with Fyniq's code, the holds and bank-detail versions sent ahead of the ordinary sync.
- **One sign-in and the switch** (NM-INT-02, 12; FR-PPL-02, 03, 18): one session per app made from the shared sign-in, sign-out and deactivation revoking
  both within 60 seconds, explicit person mapping on linking, the HR-only landing and the Finance | People switch.
- **Health and restore** (NM-INT-04, 13; FR-PPL-19): the link's state, last sync, waiting or failed events and retry, N.1 #7 on Nemura's Checks, per-run
  receipts and a reconciliation checkpoint after a restore.
- **Screen:** `nm-fyniq-link`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds `people/fyniq-integration` (NM-INT-01 to 13 and the Appendix E requirement) and the cross-app rows of
`integrations/people-platform` (FR-PPL-02, 03, 10, 15, 18), and finishes NM-ARC-04, NM-ROLE-04's across-both-apps part and NM-UX-03.

## Impact

- **Code:** `internal/nemura/link` (its own envelope code, tested against the shared fixtures), `internal/adapters/postgres/nemura`,
  `db/nemura/migrations`, `internal/nemura/api`, `contracts/openapi/nemura`, `contracts/peoplelink`, `apps/nemura-web`, `apps/web` (the switch), `cmd/nemura`.
- **Data:** the link state, a per-run receipt and checkpoint table, and an inbox in Nemura's database; the outbox gains the link's event types.
- **Needs the owner:** design D3 of `p1-payroll-link` (who verifies a payroll approver's passkey) and D11 (when the second approver applies); both default
  to the recommended option and are recorded for confirmation.
