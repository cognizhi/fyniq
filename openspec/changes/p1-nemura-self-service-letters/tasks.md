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

- [ ] 2.1 The KETs content from MOM's pages into `sg-people` as a new version, researched, with its source, quote and pinned cases. `// req: NM-PACK-06`
- [ ] 2.2 `person_terms` (D7), restricted fields sealed, the KETs template and letter type, generation from the terms and the record. `// req: NM-LTR-06`
- [ ] 2.3 N.1 #1: KETs not issued within 14 days of the start. `// req: NM-LTR-06`
- [ ] 2.4 Screens, guide and kb pages; verify.

## 3. Self-service and the leaver's account (slice 3)

- [ ] 3.1 My home (NM-ESS-01): the tiles, the balance, the next public holiday, the open requests. Screen `nm-my-home`.
- [ ] 3.2 My details (NM-ESS-03): direct changes and changes with evidence and HR approval; the audit shows what changed and never a sealed value.
- [ ] 3.3 Requests (NM-ESS-04): one list with type, status, due date and reply for letters, flexible work, details changes and HR questions. Screen `nm-my-requests`.
- [ ] 3.4 The leaver's narrow account (NM-ESS-06, D9), re-issue of a lost document. Screen `nm-leaver-home`.
- [ ] 3.5 Verify.

## 4. Automatic issue, joiners and leavers (slice 4)

- [ ] 4.1 Delegations (NM-LTR-04, D6): created by a signatory, bounded, revocable, ended by a template change; each letter keeps its bytes and who asked.
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
