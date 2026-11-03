# Tasks

Each slice below is one pull request that passes `make test` on its own, as the checks were built. Slice 1 is two:
the person on the ledger with its API (1.1 to 1.5), then the pages (1.6).

## 1. The person on every line, and director accounts (slice 1)

- [x] 1.1 Migration (D1, D2):
  - `contact.is_director`, `is_shareholder` and `member_user_id`, with `contact_role_known` widened and a member
    linked to one contact at most;
  - `journal_line.party_id`, refused by `journal_line_check` when missing on 2500 or 2120, not a director or
    shareholder, or on any other account (FY250);
  - FY249: a contact with such lines stays marked.
  Plus the storage port and adapter, attack tests, and the grants and schema goldens. `// req: FR-EQ-01`
  Built (migrations 000199 to 000201): FY251 also refuses a contact created with a member, and the API may not write the
  member on insert at all; the member changes only by `Contacts.LinkMember` under the approved request, at the contact's
  version the request names. The error code is STORAGE-PARTY-01.
- [x] 1.2 The engine and the sources (D2, D3):
  - `ledger.Line.Party`, and `LEDGER-41` for a missing or stray person;
  - manual journal and opening balance lines carry a person and origin on 2500 and 2120;
  - a bank transaction's contact is the person;
  - the sample company's generator gives its 2500 lines a person.
  Tests: each source refuses a line without its person, and replay is unchanged. `// req: FR-EQ-01`,
  `// req: ENG-06`
  Built: `ledger.Settings.People` is read with the chart for every posting, so the engine names a contact that is not a
  director or shareholder before the database does. A bank line's part on a director's account takes the line's contact
  under `bank_transaction@1`: such a part posted with no person before, which the close now names, and none exists in
  production. The replay compares each line's person. The sample company names Mei Lin Tan and Arjun Rao in turn on
  its 2500 and 2120 lines, drawing no random number, so the rest of the company is unchanged; `seedload` adds them as
  contacts.
- [ ] 1.3 The balance service (D4): `balance.People` and `balance.PersonLedger`; the close's sub-ledger check names
  lines with no person. The oracle states the per-person sub-ledger independently, and the scenario format gains
  `party` (D16). Tests: property tests (the sum by person is the control account) and a differential golden case.
  `// req: FR-EQ-01`, `// req: INV-07`, `// req: QA-ENG-02`
  Built, with the API slice: `balance.People`, `balance.PersonLines`, `director.Accounts` and `director.StatementOf`, and
  the close's check "directors", which C.2 #2 reads with the other sub-ledgers. The oracle's half follows in its own pull
  request, so this task stays open until it merges.
- [x] 1.4 The member link under dual control (D1): the approval object `contact_member_link`, never signed by the
  member linked. Contacts' roles in `createContact` and `updateContact`. Tests: through the API as each matrix role,
  and the authorisation matrix rows. `// req: FR-EQ-01`, `// req: QA-SEC-03`
  Built: `linkContactMember` (PUT /v1/contacts/{contact_id}/member) always answers 202 with the request, in every team
  mode; asking needs A on the row (the Owner, the Controller), and signing it refuses the member linked or unlinked
  (FOUNDER-04). A member signs in as one director or shareholder at most (STORAGE-PARTY-01 at asking).
- [x] 1.5 The API (D5): `listDirectorAccounts`, `getDirectorStatement` with its PDF, and `getMyDirectorAccount`.
  Then `make openapi`. Tests: each figure checked against hand-worked amounts (QA-RPT-01). `// req: FR-EQ-01`,
  `// req: FR-EQ-14`
  Built: the PDF is `exportDirectorStatement` (PDF from the render service, owed to the person shown positive, or CSV),
  audited as `report.exported`. `getMyDirectorAccount` answers `linked: false` when no one is linked to the caller, so
  Money I put in can say so; FOUNDER-03 is for a caller who is not a person signed in as themselves.
- [x] 1.6 The pages (D5):
  - Director accounts (Expert) and Money I put in (Basic);
  - the person picker on the contact record, categorise, journal and opening balances;
  - the feature registry tags, help keys, guide pages and `kb/`;
  - a Playwright journey.
  Tests: Vitest with axe, and mode parity. `// req: FR-EQ-14`, `// req: QA-UI-04`
  Built: Accounting › Director accounts (`/accounting/directors`, Expert): each person's balances owed to them shown
  positive, an account in debit, the statement by origin with its PDF and CSV, and Link member for the Owner and the
  Controller; Money I put in (`/me/money-i-put-in`, Basic, in the Owner's personal menu). Contacts gain the director
  and shareholder roles and filter; a journal or recurring journal line on 2500 or 2120 asks Whose account and Where it
  came from; the opening balances give a director's account one row per person, each with its origin. The guide page
  `docs/user-guide/director-accounts.md` is new, `founders-money.md` says where Money I put in is, and `kb/` is
  compiled. Tests: `founders.test.tsx` (Vitest with axe) and `e2e/founders.spec.ts`. The categorise screen names the
  person through the line's contact as it is; the canvases are republished with task 6.3.

## 2. Paid personally and costs before the books start (slice 2)

- [ ] 2.1 Paid personally (D6):
  - a claim's `settle_to`, refused for a claimant who is not a linked director, or with an advance (`CLAIM-33`);
  - the rule `claim@2` crediting 2500 with the gross and the person;
  - the claim editor's choice.
  The oracle states the legs. Tests: the legs with and without claimable input tax, and that the claimant never
  approves. `// req: FR-EQ-02`
- [ ] 2.2 Costs before the books start (D7): the `pre_incorporation_cost` document (receipt required, needs decision
  ⚠, the accountant's decision, posting at the books start or the decision date), its API and the Costs before
  incorporation page. Tests: posted legs, refused dates, and the decision by role. `// req: FR-EQ-03`

## 3. Pay choices, fees, exempt status and confirmations (slice 3)

- [ ] 3.1 Pay choices (D8): `director_pay_choice` (evidence required, append-only, "No salary" when none), its API
  and the Director accounts' pay choice. Tests: the choice in force by month, and the default. `// req: FR-EQ-04`
- [ ] 3.2 Directors' fees (D8): the `director_fee` document as an approval object the person paid never signs,
  posting Dr 6050 / Cr 2120 on the entitlement date. The oracle states the legs. `// req: FR-EQ-04`
- [ ] 3.3 Exempt status (D9): `exempt_status`, its API and the Director accounts' banner. Tests: the status in force
  on a date, and the words of the warning. `// req: FR-EQ-11`
- [ ] 3.4 Year-end confirmation (D13): `director_confirmation` with a passkey step-up by the linked member, and the
  year-end close checklist's item. Tests: confirmed, changed since and unlinked. `// req: FR-EQ-12`

## 4. Settlements: repay, keep and waive (slice 4)

- [ ] 4.1 The `director_settlement` document and its approval (D10):
  - the ledger impact in the envelope;
  - FM-9's exclusion (`APR-15`), and the accountant's step when every available approver benefits;
  - the default route and price basis as settings.
  Tests: who may sign in Founder and Standard mode. `// req: FR-EQ-05`, `// req: FM-9`
- [ ] 4.2 Keep and waive (D10): keep's terms with no entry; the waiver's legs to 3300 or income with the reason and
  the "deducted earlier" tag. The oracle states the legs. `// req: FR-EQ-05`, `// req: FR-EQ-07`
- [ ] 4.3 Repay (D10): the payment batch item kind `director`, the `director_repayment` posted on release to the
  person's account in force, and matching by bank import. Tests: through a batch from approval to match.
  `// req: FR-EQ-05`, `// req: FR-BNK-12`
- [ ] 4.4 The Settle a balance page, with the routes, the ledger impact and the approvals. `// req: FR-EQ-05`

## 5. Conversion and the register of share issues (slice 5)

- [ ] 5.1 `RND-SHARE` in `internal/domain/rounding`, with property tests, stated independently in the oracle (D11).
  `// req: FR-EQ-06`
- [ ] 5.2 The register of share issues (D11):
  - `share_issue`, append-only, with RLS forced;
  - the source `share_issue` allowed on share capital (INV-13, extended in the database);
  - issues before the books start recorded without an entry.
  Tests: attack tests that no other source posts share capital. `// req: FR-EQ-08`, `// req: INV-13`
- [ ] 5.3 The convert route (D11):
  - approve with the four documents, the price basis, the capacity and the measurement basis;
  - effective on the date ACRA registered it, posting from the register, refused in a closed period or after the
    approval lapsed.
  The oracle states both bases. `// req: FR-EQ-06`
- [ ] 5.4 The ACRA steps (D12) and the register's page on the equity register's screen. `// req: FR-EQ-09`

## 6. Presentation, checks and the canvases (slice 6)

- [ ] 6.1 Presentation (D14):
  - net debits under 1250;
  - non-cash labels in the cash flow;
  - shares issued for cash and non-cash apart in the movement in equity;
  - the related-party schedule.
  Tests: each figure checked, and the oracle's statements. `// req: FR-EQ-10`, `// req: FR-EQ-11`
- [ ] 6.2 The checks C.2 #21, #22, #23 and #25 on the checks engine (D15), each with a fixture that fires and one
  that passes. `// req: FR-EQ-11`, `// req: FR-EQ-09`, `// req: FR-EQ-12`, `// req: INV-13`
- [ ] 6.3 The canvases `people-ledger`, `equity`, `workflows`, `sales-purchases` and `banking-claims` rebuilt and
  republished, with the review register's area for the founders' money ([accountant to confirm] readings,
  FR-UX-41).
- [ ] 6.4 `make test` and `make check` green. The build plan and the PRD rows say what was built.

## Workflow follow-up

- Archive the change once every slice has merged (`/opsx:archive p1-founders-money`).
- The SAFE or note route, holders and the cap table follow in `p1-equity-register`.
