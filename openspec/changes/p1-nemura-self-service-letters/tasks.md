# Tasks

Each numbered group is one slice, in this order, and each ends with the local gates green: `make check`, the Go tests of the touched packages in both flavours,
`make api-check`, `make db-lint`, the web typecheck and tests. A slice never leaves a test red or a requirement tagged that its tests do not fully prove.
`[live-verify]` marks what only the cloud can show.

Load before each slice: `fyniq-feature-change`, `fyniq-api-integrations`, `fyniq-design-system` and `fyniq-knowledge-base` (every screen),
`fyniq-security-compliance` (1, 4), `fyniq-sg-statutory` (2), `fyniq-release` (a PRD revision).

## 1. Letter types, templates and issue in two steps (slice 1)

- [x] 1.1 PRD-002 revision 0.11 for D2's departure from NM-LTR-02 (Nemura's own text templates in P1), with the revision-history row and the specs stamped.
- [x] 1.2 The catalogue of letter types and merge fields (pure, tested): each type's default fields, signer and due days; the field catalogue with formats; a field
  outside a type's list is refused. `// req: NM-LTR-01`
- [x] 1.3 Migration 000023: `letter_settings` (the letterhead), `letter_type`, `letter_template_version` (draft, published immutable, numbered, approval), `letter_request`,
  `letter_issued` (append-only evidence); forced RLS, audit, goldens. `// req: NM-LTR-01, NM-LTR-02`
- [x] 1.4 The renderer port and its local adapter (D3), with a contract test both adapters pass.
- [x] 1.5 Request, HR check, signatory's passkey signing over the hash, issue with the evidence (D4), the expiring link and the employee's copy (D5); the attack
  tests (a letter changed after issue, signing without a fresh passkey, a field outside the type, an unauthorised signatory). `// req: NM-LTR-03, NM-LTR-07`
- [x] 1.6 API, screens `nm-letters`, `nm-letter-template`, `nm-request-letter`, guide and kb pages; the data inventory and retention rows.
- [x] 1.7 Verify: the local gates.
  Left in slice 1: the production renderer (the platform's render service, `[live-verify]`: the release build has none, so a letter is not checked there); the email that carries the
  link (notices are queued and logged until notifications deliver them; a person makes a link on demand); replacement and withdrawal of an issued letter as a new row; the
  contract test that both renderers pass; narrowing who may request a type; the retention lock on the letter blobs.

## 2. KETs (slice 2)

- [x] 2.1 The 17 KETs items from MOM's tripartite guidelines (read 20 Nov 2026) into `sg-people@2026.6` (`ket_items`, `kets_days_after_start`), researched, with the source,
  the quote and pinned cases; the earlier versions are kept and do not hold them. `// req: NM-PACK-06`
- [x] 2.2 Migration 000024 `person_terms` (D7, O3): effective-dated history that is only ended, once; the base terms written by the people team, the pay by payroll and the
  Owner; the KETs letter type and its standard text; the letter is filled from the terms, the working pattern, the classification and the pack, and the people team asks
  for it for a person (`POST /v1/people/{id}/letters`). `// req: NM-LTR-06`
- [x] 2.3 N.1 #1: KETs not issued 14 days after the start are ✖, a reminder in the 3 days before. `// req: NM-LTR-06`
- [x] 2.4 The terms panel on `nm-person`, guide and kb pages; PRD-002 rev 0.12; verify.
  Left in slice 2: the sealed form of the pay terms (they are guarded by role and kept out of the audit event, not sealed with the key service: [accountant to confirm] whether
  sealing is wanted before payroll holds pay); "place of work" and the probation's end as terms of their own (MOM's list has neither: the probation reads from the person's
  record); re-issuing the KETs when terms change (a new request does it); reading MOM's page rather than the PDF guidelines for any later change `[verify]`.

## 3. Self-service and the leaver's account (slice 3)

- [x] 3.1 My home (NM-ESS-01): the tiles, the annual leave left, the next public holiday, the open requests, read by the page from the API where each is kept. Screen `nm-my-home`.
  Left in 3.1: the next pay day and the payslip tile (payroll), the Claims tile and its "ask HR for Fyniq access" state (`p1-nemura-fyniq-link`, NM-ESS-05).
- [x] 3.2 My details (NM-ESS-03): migration 000025; contact details, the address and up to three emergency contacts save at once and are audited by field name, never by
  value; legal name, residency and NRIC or FIN go through a request with evidence that another HR member decides (decision O4). `// req: NM-ESS-03`
- [x] 3.3 Requests (NM-ESS-04): one list with the type, the step (received, with HR, ready, closed), the due day and the reply for letters, flexible work, changes of details
  and questions to HR; the people team's queue and replies; withdrawal. Screens `nm-my-details`, `nm-people-requests`, and `nm-ask`'s "ask HR" at `/me/ask/hr`. `// req: NM-ESS-04`
  Left in 3.2 and 3.3: the notices to the person and the people team are queued and logged until notifications deliver them; due days are constants, not company settings;
  a change of residency needs the person's legal classification to exist first.
- [x] 3.4 The leaver's narrow account (NM-ESS-06, D9, decision O5): derived from the last day, no role, own documents and letters and a question to HR, ends 12 months after the last
  day; re-issue of a lost document is that question and HR's new letter. Screen `nm-leaver-home`. `// req: NM-ESS-06`
  Left in 3.4: payslips and IR8A (payroll); a prompt for HR to end role grants at offboarding (4.2); the last Owner who leaves is locked out until recovery.
- [x] 3.5 Verify: the local gates.

## 4. Automatic issue, joiners and leavers (slice 4)

- [x] 4.1 Delegations (NM-LTR-04, D6): migration 000026; created by the Owner after a fresh passkey, bounded (a monthly limit, an end date within twelve months, one live
  delegation per type, never pay), revocable once, ended by a template change; each letter keeps its bytes, who asked and the delegation as its authority. `// req: NM-LTR-04`
  Left in 4.1: "who may ask" is the one rule (active employees not serving notice); the people assistant's drafts do not exist yet and will never use this path.
- [ ] 4.2 The joiner's invitation task on People home (NM-ONB-02) and the offboarding dates and leave settlement (NM-ONB-03, D10). Screen `nm-onboarding`.
- [ ] 4.3 Verify, and tag only what is fully proven.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-LTR-01 Letter types | 1.2, 1.3 | | |
| NM-LTR-02 Templates | 1.3 | Nemura's own editor (D2) | Fyniq's editor: the link |
| NM-LTR-03 Issue in two steps | 1.5 | | |
| NM-LTR-04 Standard letters without waiting | 4.1 | | |
| NM-LTR-06 KETs | 2.1 to 2.3 | | |
| NM-LTR-07 What an issued letter proves | 1.5 | | the verification page: P2 (NM-LTR-05) |
| NM-ESS-01 My home | 3.1 | | |
| NM-ESS-02 My pay | | not here | `p1-nemura-payroll` |
| NM-ESS-03 My details | 3.2 | | |
| NM-ESS-04 Requests | 3.3 | | |
| NM-ESS-05 Claims link | | not here | `p1-nemura-fyniq-link` |
| NM-ESS-06 After leaving | 3.4 | payslips and IR8A with payroll | `p1-nemura-payroll` |
| NM-ONB-02 Accounts | 4.2 | the task | the invitation: `p1-nemura-fyniq-link` |
| NM-ONB-03 Offboarding | 4.2 | the dates and the leave settlement | final pay and IR21: `p1-nemura-payroll` |
