# Tasks

**Status, 20 Nov 2026:** Fyniq's receiving side is built and tested (`p1-payroll-link` 2 to 5: the link with two approvers, the link's client and scopes, the person, bank-details, hold and pay-run operations, the inbox and the projection); nothing of Nemura's sender is built yet, only its copy of the envelope rule. What to build here first: 1.1 (the link record, its key, the second approver on API clients), then 2.1 and 2.2 (sign and send through the outbox to `/v1/people-link/projection-events` and `/hold-events`), then 3.1 (the approval envelope and `payroll.run.approved`, whose passkey challenge is the envelope's SHA-256 at Nemura's origin). Two open owner decisions shape it: `p1-payroll-link` design D3 (the approver's passkey is verified against Fyniq's own enrolment) and D11.

Slices on `dev/integration` (no pull requests), after or alongside `p1-payroll-link` (which builds Fyniq's side). Ticked only when fully built, with a
"Built:" note and a "**Left:**" note. Nemura's migrations are in `db/nemura/migrations` (continue from the highest number at the time). Contract
fixtures stand in for pay runs until `p1-nemura-payroll` exists. `[live-verify]` marks what needs the cloud.

## 1. The link on Nemura's side (NM-ARC-04, NM-INT-11)

- [ ] 1.1 `fyniq_link` (design D2) with states, epoch and the key reference; the Owner and a second approver link, change and unlink; the link key made and
  held by the worker role only (design D3).
  (Built in part, 21 Nov 2026: Nemura migration 000034 `fyniq_link` (states, epochs one above the last, one live link, no delete), `fyniq_link_secret` (Nemura's own Ed25519 private key, readable by the worker role alone; sealing under the platform key is `[live-verify]`), and `nemura.MakeLink`. **Left:** the Owner-and-second-approver flow and the API/page around it, and the key's rotation.)
- [ ] 1.2 The second approver on Nemura's API clients from the day the link exists (design D11 of `p1-payroll-link`): registering or revoking a client needs
  another Owner or Admin with a fresh passkey; a standalone organisation keeps the single Owner.
- [ ] 1.3 Unlinking settles work in flight first; a credential or event of an earlier epoch is refused.

## 2. Events and the inbox (NM-INT-01, 05, 10; FR-PPL-15)

- [ ] 2.1 `internal/nemura/link`: the envelope and its canonical bytes tested against `contracts/peoplelink/` fixtures; the outbox signs at send time.
  (Built in part, 20 Nov 2026: `internal/nemura/link` (canonical bytes, digest, Ed25519 signature, the six checks) passes the same fixtures as Fyniq's copy (`contracts/peoplelink/`). **Left:** the outbox signing at send time, the key custody (1.1), and the worker's egress call with the link client's token: the receiving side is built and described in `contracts/peoplelink/README.md`, so the sender can be written against it. Not tagged: NM-INT-10 needs the inbox and gap repair on Nemura's side too.)
  (Built, continued: `nemura.SignOutbound` signs an event for Fyniq with the link's key at the next sequence number, audited by type and number, never a payload; `nemura.TaxHoldChanged` builds `people.tax_hold.changed` from the latest row of `nemura.tax_hold` and nothing else, and its signature verifies with Nemura's public key. **Left:** the outbox row and the worker's egress call with the link client's token, the priority queue (design D5), and the person, bank-details and payroll-run senders.)
- [ ] 2.2 `people.person.upserted` (with the line manager and tombstones), `people.bank_details.changed`, `people.tax_hold.changed` (priority, design D5)
  and `leave.taken.changed`, with only Appendix E's fields: a test that no NRIC, FIN, medical, disciplinary or review field can reach an event.
- [ ] 2.3 The inbox for `payroll.run.posted`, `payroll.payments.status` and `people.department.changed`: dedupe, versions, gap repair, replay window.

  (Built in part, 21 Nov 2026: migration 000034 `link_inbox` (each event once by its ID, identities and a digest, append-only) and `link_run_posting`; `nemura.ReceiveLinkEvent` verifies the envelope against the link (organisations, epoch, 5-minute window, Fyniq's signature, digest), answers a repeat as `duplicate`, reads the payload strictly, sends `payroll.payments.status` through `RecordPaymentState` (the one function of `nemura.pay_payment_event`, source `fyniq`) and keeps a run's `payroll.run.posted` answer; a refusal after the signature held is kept in the inbox. `TestTheLinkReceivesFyniqsEventsOnceAndSignsTheHoldsItSends`. **Left:** the HTTP operation that calls it (a Nemura API client for Fyniq with its own scope), Fyniq's sender of these events (its own signing key and an outbox job), `people.department.changed`, gap repair and the run page showing the posting answer.)
## 3. Runs and their proof (NM-INT-03, 06, 07, 08, 09)

- [ ] 3.1 The approval envelope over a run's canonical figures and `payroll.run.approved` and `payroll.payments.ready` built from contract fixtures; the
  preparer is not the approver and cannot approve or release the batch in either app (NM-INT-07).
- [ ] 3.2 A refused run shows Fyniq's code and a plain message and waits (NM-INT-03, 06); the posting reference shows on the run.
- [ ] 3.3 Holds travel at once and bank details by version, never the number (NM-INT-08, 09); unlinking moves open holds to Fyniq.

## 4. One sign-in, the switch and sessions (NM-INT-02, 12; FR-PPL-02, 03, 18)

- [ ] 4.1 Explicit person mapping on linking; no match by email; no Fyniq role granted.
- [ ] 4.2 Sign-out and deactivation revoke both apps' sessions and live streams within 60 seconds (design D6).
- [ ] 4.3 The Finance | People switch in Fyniq's header and Nemura's; the HR-only landing on People home; an employee who only claims sees no switch.

## 5. Health, restore and the screen (NM-INT-04, 13; FR-PPL-19)

- [ ] 5.1 Link health on both apps' Checks (N.1 #7) and the alerts; retry of failed events.
- [ ] 5.2 Per-run receipts and the reconciliation checkpoint; restoring each database alone and both holds the work after the restore point as unknown;
  privacy deletions applied again.
- [ ] 5.3 `nm-fyniq-link` as designed; help keys; user-guide pages compiled into `kb/`.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| FR-PPL-02 Workspace switch and HR landing | 4.3 | planned | |
| FR-PPL-03 One sign-in with Nemura | 2, 4 | planned | |
| FR-PPL-10 Segregation across both apps | 3.1 (with `p1-payroll-link` 4.2) | planned | |
| FR-PPL-15 Events between the apps | 2 (with `p1-payroll-link` 1, 3) | planned | |
| FR-PPL-18 Sessions and identities with Nemura | 4 | planned | |
| NM-INT-01 Sync | 2.2 | planned | |
| NM-INT-02 One sign-in, one switch | 4 | planned | |
| NM-INT-03 Pay runs to the ledger | 3.2 | planned | |
| NM-INT-04 Health | 5.1 | planned | |
| NM-INT-05 Minimum data to Fyniq | 2.2 | planned | |
| NM-INT-06 Approved runs carry their proof | 3.1 | planned | |
| NM-INT-07 One person across both apps | 3.1 | planned | |
| NM-INT-08 Tax-clearance holds | 3.3 | planned | |
| NM-INT-09 Bank details travel with their version | 3.3 | planned | |
| NM-INT-10 Event envelope | 2.1 | planned | |
| NM-INT-11 Link lifecycle | 1 | planned | |
| NM-INT-12 Identity and sessions across the apps | 4 | planned | |
| NM-INT-13 Restore and reconcile | 5.2 | planned | |
| Events and data shared with Fyniq (unnumbered) | 2 | planned | |
| NM-ARC-04, NM-ROLE-04 (across both apps), NM-UX-03 | 1, 3.1, 4.3 | planned | |
