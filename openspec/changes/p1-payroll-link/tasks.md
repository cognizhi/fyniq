# Tasks

Each numbered group is one or more slices on `dev/integration` (no pull requests; the local gates are the gate), in this order. A task is ticked only
when fully built; a "Built:" note says what, a "**Left:**" note what remains. `[live-verify]` marks what only a real bank, IRAS, the CPF Board or the
cloud can show. Contract fixtures stand in for Nemura's pay runs. Migrations 000900 to 000949. Decisions D1 to D12 are in design.md; D3 and D11 are
owner-to-confirm.

Load before each slice: `fyniq-feature-change`, `fyniq-api-integrations` (1, 2), `fyniq-accounting-domain` (5, 6), `fyniq-security-compliance` (2, 4, 5, 7),
`fyniq-design-system` and `fyniq-knowledge-base` (8), `fyniq-release` (PRD revisions).

## 1. The contract and its fixtures (FR-PPL-15 and 03 are counted in `p1-nemura-fyniq-link`, built here because Fyniq's side needs them first)

- [x] 1.1 The envelope and its canonical bytes (design D2): `internal/domain/peoplelink` (digest, signature, window, epoch) and the shared fixtures in
  `contracts/peoplelink/` generated at test time; negative tests for an altered payload, an altered header, a wrong key, an old epoch and a 5-minute skew.
  (Built, 20 Nov 2026: `internal/domain/peoplelink` and Nemura's own copy `internal/nemura/link` (canonicalisation, digest, Ed25519 signature, the six checks with codes `PPL-01` to `PPL-06`); `contracts/peoplelink/` with `canonical.json` (hand-worked vectors and refusals), `envelope.json` (a signed envelope; its key is derived at test time) and a README. Both copies pass the same fixtures; tests do not carry a `req:` tag until FR-PPL-15 and NM-INT-10 are complete.)
- [ ] 1.2 The events and calls of Appendix E as operations in `contracts/openapi/` (`/v1/people-link/...`) and event schemas under `contracts/events/`;
  `make openapi`, `make api-check`; the error codes are stable and plain.

## 2. The link in Fyniq (FR-PPL-01, 16, 08, 03, 18)

- [ ] 2.1 Migration: `people_link` (state, epoch, Nemura organisation, link public key, client, source per feature), tenancy and RLS, audited.
  (Built, 20 Nov 2026: migration 000900 `people_link` (states active, suspended, unlinking, unlinked; epochs one above the last, never reused; one live link; no delete; key and approval kept) and `people_source` (the latest row of each feature is in force), `storage.PeopleLinks` with `Link`, `SetState`, `SetSource`, `Sources`; `TestAttackTheLinkKeepsItsEpochsAndItsTenant` carries `// req: FR-PPL-16`... not tagged complete. **Left:** the Owner-and-second-approver request and its executor, the link client's registration, the Settings page (2.2, 2.3).)
- [ ] 2.2 Settings → People and HR: sources per feature; link, change source and unlink need the Owner and a second approver; unlinking settles work in
  flight first (runs approved not posted, unknown outcomes, open holds, events waiting) and revokes the client and key after.
- [ ] 2.3 The link client (design D1, D4): registered only by the dual-approved link, Appendix E scopes only, principal class `link`; attack tests that no
  PAT, integration client, agent client or MCP token reaches a link operation or holds a link scope.
- [ ] 2.4 Link health: last sync, events waiting or failed, retry, the 24-hour notice to the Owner and Payroll administrator, the 1-hour alert near pay day.
- [ ] 2.5 Sessions and identities (FR-PPL-18, counted in `p1-nemura-fyniq-link`; its Fyniq half is built here): people matched by issuer, subject and tenant, mapped explicitly on linking, never by email; no role granted.

## 3. The inbox and the employee projection (FR-PPL-15, 06)

- [ ] 3.1 `people_link_inbox` (design D5): dedupe by event ID, per-object versions, older ignored and recorded, tombstones, gap repair by reading the
  object; a replay outside 5 minutes or a bad signature refused; no pay or bank payload ever stored or logged.
  (Built, 20 Nov 2026: `people_link_inbox` (append-only, the event ID once, outcome applied, ignored as older or refused with a code, identities and digests only) and `PeopleLinks.Receive`; the projection's per-part version rules (`peoplelink.ApplyPerson`, `ApplyBankDetails`, `ApplyTaxHold`). **Left:** the receiving operation that verifies the envelope and calls them, gap repair, the redaction test on logs (3.1 stays open).)
- [ ] 3.2 The projection: person, name, email, department, cost centre, dates, status, line manager, hold state and version, bank-detail version (never the
  account number); `people.person.upserted` and `people.bank_details.changed` applied through the inbox.
  (Built, 20 Nov 2026: `person_projection` (person, name, email, department and cost-centre values, dates, status, line manager as a person ID, tombstone, hold and bank-detail versions; never an account number; an older version of a part refused by the database), `PeopleLinks.Person`, `People`, `PersonOfUser`, `SavePerson` audited by field name, never by value (`TestAttackTheProjectionOnlyMovesForwardAndTheInboxReceivesOnce`). **Left:** applying events through the API (3.1) and the explicit person-to-user mapping flow (2.5).)
- [ ] 3.3 Leave for the books (FR-PPL-07): `leave.taken.changed` (dates and hours, never the type; a tombstone restores the hours) lowers available hours
  once against holidays (FR-PRJ-12); the month-end accrual arrives as a draft journal (P2 part).
- [ ] 3.4 What reads the projection: claim routing to the line manager (FR-AP-17), the approvers' department figures, the personal approvals page of
  `p1-approval-engine` 5.1; project cost rates stay in Fyniq, restricted (FR-PAY-13).

## 4. Verified payroll authority and segregation (FR-PPL-09, 10)

- [ ] 4.1 The checks in the posting transaction (design D3, D6): link signature and epoch, payload digest against the envelope, the approver's passkey
  record, approver is not the preparer and held the right, the mapping version, the envelope unused; each failure refuses with its own stable code, no
  side effect, and the refusal audited.
- [ ] 4.2 Segregation as one chain (SOD-4; FR-PPL-10 is counted in `p1-nemura-fyniq-link`, whose Nemura half proves the other side): the preparer cannot approve or release the salary batch under any account (issuer and subject), the Founder-mode
  two-person rule applies, and break-glass cannot change a salary account, lift a hold or pay the person using it.

## 5. Posting a pay run (FR-PPL-04, 11; FR-PAY-07; the payroll posting rules)

- [ ] 5.1 The rule `payroll_run@1` (design D7) with replay registration, the oracle's half written from PRD-001 §10.2 and B.3, the scenario schema and a
  differential scenario; the full `make differential` passes.
  (Built, 20 Nov 2026: `ledger.PostPayrollRun` (`payroll_run@1`: the difference of a version from what the run's earlier versions posted; departments with dimension values, employer CPF, SDL, 2110, 2100, deductions, reimbursements; stale refused; `LEDGER-51`), `ReplayPayrollRun`; the oracle's `sources/payroll.py` written from PRD-001 §10.2 and B.3; the scenario schema, `golden/payroll.json`, `oracle/tests/test_payroll.py` and a Hypothesis strategy (`payroll` in `DIFFERENTIAL_SOURCES`); the full `make differential` passes. FR-PAY-07 is not tagged yet: its tests say "part of", until the verified path posts through it (5.3). **Left:** the replay registration over a stored run version waits for the run's table (5.3), and the Appendix B.3 per-employee-month formulas are `p1-nemura-payroll`'s.)
- [ ] 5.2 Corrections: the difference of a new version, a stale version refused, out-of-order convergence, a closed wage month posted in the open month,
  a withdrawn run reversed; economic uniqueness by organisation, source system, run and wage month.
- [ ] 5.3 The mapping version (design D8) and the pay-run receipts: posting reference and status back to Nemura (`payroll.run.posted`), a refused posting
  returns its code.

## 6. Salary payments (FR-PAY-11, FR-PPL-12, 13, 14, 17; C.2 #44)

- [ ] 6.1 The salary batch on the payment batch (design D9): lines sealed, need to know, bank-detail versions in the approval envelope, a newer version
  blocks the line until re-approval.
- [ ] 6.2 Paid means paid: line states, instruction IDs, the bank feed or evidence as the only source of "paid", `payroll.payments.status` back to
  Nemura, an unknown outcome blocks a new instruction.
- [ ] 6.3 The hold check at every release (design D10): salary, claims, advances and director balances; no answer in 5 seconds or an older version waits.

## 7. Without Nemura and the checks (FR-PPL-05, FR-PAY-15; C.1 #19, #20, #21; C.2 #14, #24, #45)

- [ ] 7.1 A provider's journal or file import as a draft journal, idempotent per period and version, with a preview (design D12).
- [ ] 7.2 The payroll go-live gate (FR-PAY-15) and the checks C.1 #19 to #21, C.2 #14, #24 (a director's pay deferred without a choice, FM-handoff from
  `p1-founders-money`) and #45.

## 8. Screens, help and the close

- [ ] 8.1 `people-hr-settings`, `payroll-overview`, `pay-run`, `salary-batch`, `employee-record` as designed; help keys, user-guide pages compiled into `kb/`.
- [ ] 8.2 Threat model and controls updated; the PRD revision; the close of this change.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| FR-PPL-01 People and HR settings | 2.2 | planned | |
| FR-PPL-04 Pay runs from Nemura | 5 | planned | |
| FR-PPL-05 External payroll and HR systems | 7.1 | planned | |
| FR-PPL-06 Employee projection | 3.2 | planned | |
| FR-PPL-07 Leave for the books | 3.3 | planned | |
| FR-PPL-08 Link health | 2.4 | planned | |
| FR-PPL-09 Verified payroll authority | 4.1 | planned | |
| FR-PPL-11 Corrections from Nemura | 5.2 | planned | |
| FR-PPL-12 Paid means paid | 6.2 | planned | |
| FR-PPL-13 Tax-clearance hold at release | 6.3 | planned | |
| FR-PPL-14 Salary bank details by version | 6.1 | planned | |
| FR-PPL-16 Link lifecycle and source changes | 2.2 | planned | |
| FR-PPL-17 Salary payments on a need-to-know basis | 6.1 | planned | |
| FR-PPL-19 Restores across the two databases | 5.3 (receipts), `p1-nemura-fyniq-link` 5 (checkpoint) | planned | |
| FR-PAY-07 Pay run approval and posting | 5.1 | planned | |
| FR-PAY-11 Salary payment batch | 6.1 | planned | |
| FR-PAY-15 Payroll go-live gate | 7.2 | planned | |
| C.1 #19, #20, #21; C.2 #14, #24, #45 | 7.2 | planned | |
| C.2 #44 Payment during an IR21 clearance hold | 6.3 | planned | |
