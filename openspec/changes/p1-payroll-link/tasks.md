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
  (Built, 20 Nov 2026: migration 000900 `people_link` (states active, suspended, unlinking, unlinked; epochs one above the last, never reused; one live link; no delete; key and approval kept) and `people_source` (the latest row of each feature is in force), `storage.PeopleLinks` with `Link`, `SetState`, `SetSource`, `Sources`; `TestAttackTheLinkKeepsItsEpochsAndItsTenant` says "part of" until the lifecycle is complete.)
- [ ] 2.2 Settings → People and HR: sources per feature; link, change source and unlink need the Owner and a second approver; unlinking settles work in
  flight first (runs approved not posted, unknown outcomes, open holds, events waiting) and revokes the client and key after.
  (Built, 20 Nov 2026: `getPeopleLink` (where each feature lives, the state and epoch, the inbox by outcome), `requestPeopleLink` and `requestPeopleUnlink` (an Admin or the Owner asks; the Owner and a second approver approve: new approval object `people_link`, migration 000901/000902; the executor registers the client, records the link at the next epoch and each feature's source; unlinking revokes the client and ends the link). `TestThePeopleLinkCarriesSignedEventsThroughItsOwnClient` runs it against the pinned Hydra. **Left:** changing a feature's source after linking, with its start day and year-to-date state (FR-PPL-16's source change), and settling work in flight before unlinking: `peopleLinkInFlight` counts nothing until runs, salary payments and holds exist (6, 5.3); the web page (8.1).)
- [x] 2.3 The link client (design D1, D4): registered only by the dual-approved link, Appendix E scopes only, principal class `link`; attack tests that no
  PAT, integration client, agent client or MCP token reaches a link operation or holds a link scope.
  (Built, 20 Nov 2026: api_client kind `link` (CHECKs: only the link's six scopes, nothing from `fyniq.*`; an integration client or a personal token cannot hold one), `authn.Principal.Link` (acts for no member; its roles and user are empty), `Policy.LinkScope`: an event operation admits only the link's client holding that scope, and the link's client reaches no other operation. `TestAttackOnlyTheLinkHoldsTheLinkScopes`, `TestNoOneButTheLinksClientReachesAnEventOperation` (user, integration and agent principals claiming the scopes, and the link's client against the ordinary operations) and the matrix rows. **Left:** the MCP surface's own refusal is the `mcpsurface` map, which lists no link operation; a red-team case through the MCP server is `p1-mcp-first-party`'s suite. Ticked: every named attack has its test.)
- [ ] 2.4 Link health: last sync, events waiting or failed, retry, the 24-hour notice to the Owner and Payroll administrator, the 1-hour alert near pay day.
- [ ] 2.5 Sessions and identities (FR-PPL-18, counted in `p1-nemura-fyniq-link`; its Fyniq half is built here): people matched by issuer, subject and tenant, mapped explicitly on linking, never by email; no role granted.

## 3. The inbox and the employee projection (FR-PPL-15, 06)

- [ ] 3.1 `people_link_inbox` (design D5): dedupe by event ID, per-object versions, older ignored and recorded, tombstones, gap repair by reading the
  object; a replay outside 5 minutes or a bad signature refused; no pay or bank payload ever stored or logged.
  (Built, 20 Nov 2026: `people_link_inbox` (append-only, the event ID once, outcome applied, ignored as older or refused with a code, identities and digests only) and `PeopleLinks.Receive`; the projection's per-part version rules (`peoplelink.ApplyPerson`, `ApplyBankDetails`, `ApplyTaxHold`). **Left:** the receiving operation that verifies the envelope and calls them, gap repair, the redaction test on logs (3.1 stays open).)
  (Built, 20 Nov 2026, continued: `receivePeopleProjectionEvent` and `receivePeopleHoldEvent` verify the envelope against the link as Fyniq holds it (organisations, epoch, 5-minute window, signature, digest), then apply once: a repeat answers `duplicate`, a version no newer is `ignored_older`, a field Appendix E does not list is refused (`PPL-09`, NM-INT-05); a refusal is logged by code and never carries the payload. **Left:** gap repair (reading the object's current state through Nemura's API) and the log-redaction test, which need Nemura's read API (`p1-nemura-fyniq-link` 2.3); so 3.1 stays open.)
- [ ] 3.2 The projection: person, name, email, department, cost centre, dates, status, line manager, hold state and version, bank-detail version (never the
  account number); `people.person.upserted` and `people.bank_details.changed` applied through the inbox.
  (Built, 20 Nov 2026: `person_projection` (person, name, email, department and cost-centre values, dates, status, line manager as a person ID, tombstone, hold and bank-detail versions; never an account number; an older version of a part refused by the database), `PeopleLinks.Person`, `People`, `PersonOfUser`, `SavePerson` audited by field name, never by value (`TestAttackTheProjectionOnlyMovesForwardAndTheInboxReceivesOnce`). **Left:** applying events through the API (3.1) and the explicit person-to-user mapping flow (2.5).)
  (Built, continued: person, bank-details and hold events apply through the inbox and the projection; a late older version never sets the manager back (`TestThePeopleLink...`).)
- [ ] 3.3 Leave for the books (FR-PPL-07): `leave.taken.changed` (dates and hours, never the type; a tombstone restores the hours) lowers available hours
  once against holidays (FR-PRJ-12); the month-end accrual arrives as a draft journal (P2 part).
- [ ] 3.4 What reads the projection: claim routing to the line manager (FR-AP-17), the approvers' department figures, the personal approvals page of
  `p1-approval-engine` 5.1; project cost rates stay in Fyniq, restricted (FR-PAY-13).

## 4. Verified payroll authority and segregation (FR-PPL-09, 10)

- [ ] 4.1 The checks in the posting transaction (design D3, D6): link signature and epoch, payload digest against the envelope, the approver's passkey
  record, approver is not the preparer and held the right, the mapping version, the envelope unused; each failure refuses with its own stable code, no
  side effect, and the refusal audited.
  (Built, 20 Nov 2026: `peoplelink.ParseRun` and `submitPayrollRun` (`POST /v1/people-link/payroll-runs`, scope `payroll.journal.submit`): the link's envelope, the approval envelope's SHA-256 against its canonical bytes, the linked organisation, preparer not approver, the approver's right, the approver's passkey assertion over that digest against a passkey enrolled in Fyniq (matched by issuer and subject; `passkey.ReverifyAt` at the link's `nemura_origin`), the mapping version, the envelope unused, the version newer; each failure has its own code `PPL-11` to `PPL-19`, posts nothing and is audited in a transaction of its own (`payroll_run.refused`, no figure). `TestAnApprovedPayRunPostsOnlyAfterItsAuthorityVerifies` refuses altered totals under an earlier approval, the preparer as approver, an approver without the right, another organisation, another mapping version, a stranger's passkey, a wrong origin, a digest the passkey did not sign and an approver with no identity. FR-PPL-09 is not tagged: its scenario "Imports stay drafts" waits for 7.1. **Left:** the approver's right is the envelope's own statement signed by the link (Fyniq cannot read Nemura's roles); the unused-envelope claim shares the transaction, a concurrent replay loses at the unique key.)
- [ ] 4.2 Segregation as one chain (SOD-4; FR-PPL-10 is counted in `p1-nemura-fyniq-link`, whose Nemura half proves the other side): the preparer cannot approve or release the salary batch under any account (issuer and subject), the Founder-mode
  two-person rule applies, and break-glass cannot change a salary account, lift a hold or pay the person using it.

  (Built in part: the preparer cannot be the approver in the same run (`PPL-13`, and the receipt table's own CHECK). **Left:** the salary batch's approver and releaser against the run (6.1), Founder mode FM-1, and the break-glass refusals, which need the salary batch.)
## 5. Posting a pay run (FR-PPL-04, 11; FR-PAY-07; the payroll posting rules)

- [x] 5.1 The rule `payroll_run@1` (design D7) with replay registration, the oracle's half written from PRD-001 §10.2 and B.3, the scenario schema and a
  differential scenario; the full `make differential` passes.
  (Built, 20 Nov 2026: `ledger.PostPayrollRun` (`payroll_run@1`: the difference of a version from what the run's earlier versions posted; departments with dimension values, employer CPF, SDL, 2110, 2100, deductions, reimbursements; stale refused; `LEDGER-51`), `ReplayPayrollRun`; the oracle's `sources/payroll.py` written from PRD-001 §10.2 and B.3; the scenario schema, `golden/payroll.json`, `oracle/tests/test_payroll.py` and a Hypothesis strategy (`payroll` in `DIFFERENTIAL_SOURCES`); the full `make differential` passes. FR-PAY-07 is not tagged yet: its tests say "part of", until the verified path posts through it (5.3). **Left:** the replay registration over a stored run version (`ReplayPayrollRun` exists; wiring it into the replay walk needs the receipt's source rows, `internal/domain/journal/replay.go`), and the Appendix B.3 per-employee-month formulas are `p1-nemura-payroll`'s.)
- [ ] 5.2 Corrections: the difference of a new version, a stale version refused, out-of-order convergence, a closed wage month posted in the open month,
  a withdrawn run reversed; economic uniqueness by organisation, source system, run and wage month.
  (Built, 20 Nov 2026: a version posts the difference from the run's receipt totals (`ledger.PayrollPosted.Encode`), a stale version is refused (`PPL-18`), a version that changes nothing posts nothing and is recorded, a closed wage month's difference posts in the current open month with the wage month in the memo, and the receipt's unique keys make one run-version and one envelope exist once. **Left:** a run withdrawn before payment is reversed (FR-GL-12) and the supplementary salary line for net pay still unpaid (6.2); the closed-month rule is proved at the API only.)
- [ ] 5.3 The mapping version (design D8) and the pay-run receipts: posting reference and status back to Nemura (`payroll.run.posted`), a refused posting
  returns its code.

  (Built, 20 Nov 2026: migration 000903/000904: `payroll_mapping` and its entries by version (department value or deduction kind to account; `getPayrollMapping`, `setPayrollMapping`), `payroll_run_receipt` (envelope digest, run, version, wage month, approver and preparer by issuer and subject, mapping version, entry, posted totals; no per-person figure) and a deferred trigger that refuses any `payroll_run` entry without its receipt from the same transaction (`TestAttackAPayrollEntryNeedsItsReceipt`). **Left:** `payroll.run.posted` back to Nemura with the posting reference or the ledger's code (needs Nemura's webhook, `p1-nemura-fyniq-link` 2.3), the mapping's dual control (it is a Payroll administrator's step-up change today: owner to confirm), and FR-EMP-02's departments page, which stays `p1-nemura-payroll`'s.)
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
| FR-PPL-01 People and HR settings | 2.2 | API and approvals built; the page is 8.1 | 8.1 |
| FR-PPL-04 Pay runs from Nemura | 5 | the posting built; the salary batch and the status back to Nemura open | 6, `p1-nemura-fyniq-link` |
| FR-PPL-05 External payroll and HR systems | 7.1 | planned | |
| FR-PPL-06 Employee projection | 3.2 | store, rules and event application built; the explicit person mapping and claim routing open | 2.5, 3.4 |
| FR-PPL-07 Leave for the books | 3.3 | planned | |
| FR-PPL-08 Link health | 2.4 | planned | |
| FR-PPL-09 Verified payroll authority | 4.1 | every check built and refused by its own test; scenario "Imports stay drafts" open | 7.1 |
| FR-PPL-11 Corrections from Nemura | 5.2 | difference, stale, closed month built; withdrawal and supplementary line open | 5.2 Left, 6.2 |
| FR-PPL-12 Paid means paid | 6.2 | planned | |
| FR-PPL-13 Tax-clearance hold at release | 6.3 | planned | |
| FR-PPL-14 Salary bank details by version | 6.1 | planned | |
| FR-PPL-16 Link lifecycle and source changes | 2.2 | link, unlink and epochs built; settling work in flight and a source change open | 6, 2.2 Left |
| FR-PPL-17 Salary payments on a need-to-know basis | 6.1 | planned | |
| FR-PPL-19 Restores across the two databases | 5.3 (receipts), `p1-nemura-fyniq-link` 5 (checkpoint) | planned | |
| FR-PAY-07 Pay run approval and posting | 5.1 | planned | |
| FR-PAY-11 Salary payment batch | 6.1 | planned | |
| FR-PAY-15 Payroll go-live gate | 7.2 | planned | |
| C.1 #19, #20, #21; C.2 #14, #24, #45 | 7.2 | planned | |
| C.2 #44 Payment during an IR21 clearance hold | 6.3 | planned | |
