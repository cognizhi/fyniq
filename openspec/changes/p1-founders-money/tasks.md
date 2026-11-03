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
- [x] 1.3 The balance service (D4): `balance.People` and `balance.PersonLedger`; the close's sub-ledger check names
  lines with no person. The oracle states the per-person sub-ledger independently, and the scenario format gains
  `party` (D16). Tests: property tests (the sum by person is the control account) and a differential golden case.
  `// req: FR-EQ-01`, `// req: INV-07`, `// req: QA-ENG-02`
  Built, with the API slice: `balance.People`, `balance.PersonLines`, `director.Accounts` and `director.StatementOf`, and
  the close's check "directors", which C.2 #2 reads with the other sub-ledgers. The oracle's half, in its own pull
  request: the scenario format's `people`, a line's `party` and `origin` and the two roles; the result's line `party` and
  `people` totals; rule 9 on both sides, the generator drawing director's accounts in half of its charts, and the seed's
  golden scenarios regenerated. Tests: `oracle/tests/test_founders.py` worked by hand, and the gate (`make differential`).
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

- [x] 2.1 Paid personally (D6):
  - a claim's `settle_to`, refused for a claimant who is not a linked director, or with an advance (`CLAIM-36`);
  - the rule `claim@2` crediting 2500 with the gross and the person;
  - the claim editor's choice.
  The oracle states the legs. Tests: the legs with and without claimable input tax, and that the claimant never
  approves. `// req: FR-EQ-02`
  Built: the rule is `expense_claim@2`, and `expense_claim@1` refuses a claim kept as a loan. The claimant's person is
  the contact linked to them, read when the claim is prepared; a director's account is refused as a claim's category;
  `recordClaimReimbursement` refuses a claim kept as a loan (it owes nothing on 2150); the draft's ledger impact names
  the person. The oracle's scenario format gains `loan` on `expense_claim` and the `director_loan` key, with a golden
  case in `pending/claims.json` (claims still wait for `cmd/scenario-run`); each side credits 2500 as it credits 2150.
- [x] 2.2 Costs before the books start (D7): the `pre_incorporation_cost` document (receipt required, needs decision
  ⚠, the accountant's decision, posting at the books start or the decision date), its API and the Costs before
  incorporation page. Tests: posted legs, refused dates, and the decision by role. `// req: FR-EQ-03`
  Built: the document is numbered PIC- (migrations 000202 and 000203). An Owner or the Controller records and changes it
  (A on "Director accounts"), and the accountant decides it (P on the row): D7 named the Controller too, whom Appendix D
  gives A, not P, on that row. The member linked to the person who paid it never decides it (FOUNDER-10). The decision
  needs a receipt (FOUNDER-08) and posts under `pre_incorporation_cost@1` (LEDGER-42); a decided cost changes no more
  (FOUNDER-07), so a correction is a journal to the person's account. Its receipts sit on the cost's own row, as a card
  line's do. The oracle states the legs (`pending/founders.json`). The "before GST registration" flag waits for
  `p1-gst`, as D7 says.

## 3. Pay choices, fees, exempt status and confirmations (slice 3)

- [x] 3.1 Pay choices (D8): `director_pay_choice` (evidence required, append-only, "No salary" when none), its API
  and the Director accounts' pay choice. Tests: the choice in force by month, and the default. `// req: FR-EQ-04`
  Built: a pay choice is a document (migrations 000204 and 000205), drafted by an Owner or the Controller (A on the row),
  recorded once its signed decision is attached (FOUNDER-13) and posting no entry; recorded, it changes no more
  (FOUNDER-14). The choice in force is the latest month on or before the day, the later recording winning within a
  month; each director account shows it as `pay_choice`, "No salary" by default. The guide's pay choice moved from a
  setting to the Director accounts' card.
- [x] 3.2 Directors' fees (D8): the `director_fee` document as an approval object the person paid never signs,
  posting Dr 6050 / Cr 2120 on the entitlement date. The oracle states the legs. `// req: FR-EQ-04`
  Built: the fee (migrations 000206 and 000207) is recorded by an Owner or the Controller (A on the row) with the
  shareholders' resolution attached (FOUNDER-17), and `requestDirectorFeePost` asks another person who holds A to
  approve it, never the requester nor the member linked to the director paid (FOUNDER-19; the database's
  `director_fee_approved`, FY252). The approval posts it under `director_fee@1` (LEDGER-43), numbered DF-; the account
  defaults to 6050; a posted fee changes no more (FOUNDER-18). The oracle's scenario format gains `director_fee` and
  the `deferred_remuneration` key (`pending/founders.json`). `db/README.md` now lists the founders' backstops, FY249 to
  FY252.
- [x] 3.3 Exempt status (D9): `exempt_status`, its API and the Director accounts' banner. Tests: the status in force
  on a date, and the words of the warning. `// req: FR-EQ-11`
  Built: the status is a document (migrations 000208 and 000209), drafted by an Owner or the Controller, recorded once
  its evidence is attached (FOUNDER-22) and posting no entry; recorded, it changes no more (FOUNDER-23). The director
  accounts show the status in force (`exempt_status`, "not_recorded" when none) and, on an account in debit, the
  warning in words (`debit_warning`), which Money I put in shows too. C.2 #21 and its re-run on a new status come with
  the checks in task 6.2.
- [x] 3.4 Year-end confirmation (D13): `director_confirmation` with a passkey step-up by the linked member, and the
  year-end close checklist's item. Tests: confirmed, changed since and unlinked. `// req: FR-EQ-12`
  Built: `confirmMyDirectorAccount` (migrations 000210 and 000211) keeps the balances held at a year's end, posted at
  once and append-only; it needs the member linked (FOUNDER-03), a step-up by passkey in the last 5 minutes (FOUNDER-26)
  and a year that has ended (FOUNDER-24). Each director account shows `year_end` (confirmed, changed since, not
  confirmed) for the latest year end; Money I put in asks the founder to confirm, running the passkey step-up when
  needed. The year-end item is a close check, `directors-confirmed` (⚠), at a financial year's last month, naming who
  must confirm and who has no member linked; C.2 #23 on the checks engine comes in task 6.2.

## 4. Settlements: repay, keep and waive (slice 4)

- [x] 4.1 The `director_settlement` document and its approval (D10):
  - the ledger impact in the envelope;
  - FM-9's exclusion (`APR-15`), and the accountant's step when every available approver benefits;
  - the default route and price basis as settings.
  Tests: who may sign in Founder and Standard mode. `// req: FR-EQ-05`, `// req: FM-9`
  Built: the settlement (migrations 000212 and 000213, numbered DS- when decided) is drafted by an Owner or the
  Controller (A on the row) for at most what is owed today, its agreement attached (FOUNDER-30), and asked for with
  `requestDirectorSettlementApproval`, whose envelope carries the waiver's legs (FR-APR-02). A signer may not be the
  person settled (APR-15; the database's `director_settlement_approved`, FY253), in every team mode. When every member
  who could sign is a director with a settlement waiting or decided in the last 30 days, the request gains a final step
  the Accountant signs [accountant to confirm]. The defaults are the settings `founders.default_route` (keep) and
  `founders.price_basis` (latest priced round), of a new kind, choice. Repay and convert are refused until tasks 4.3
  and 5.3. The tests sign in Founder mode; the refusal and the database's guard take the same path in Standard mode,
  which no test switches to yet.
- [x] 4.2 Keep and waive (D10): keep's terms with no entry; the waiver's legs to 3300 or income with the reason and
  the "deducted earlier" tag. The oracle states the legs. `// req: FR-EQ-05`, `// req: FR-EQ-07`
  Built: a loan kept is decided with no entry, its terms kept (interest, repayment day or on demand, subordinated); a
  waiver posts `director_settlement@1` (LEDGER-44) on the day approved, Dr 2500/2120 naming the person (waived), Cr the
  account chosen; a waiver of 2120 is tagged `deducted_earlier` [verify]. The oracle's `director_waiver` states the legs.
- [x] 4.3 Repay (D10): the payment batch item kind `director`, the `director_repayment` posted on release to the
  person's account in force, and matching by bank import. Tests: through a batch from approval to match.
  `// req: FR-EQ-05`, `// req: FR-BNK-12`
  Built:
  - **The route.** It is drafted only while the person has an approved SGD account at a Singapore bank (FOUNDER-32):
    the linked member's own, else the contact's as a vendor. The request's diff shows the account (`repay_to`).
  - **What is owed.** A settlement may not exceed what is owed less what other settlements approved to be repaid still
    have open.
  - **Decision.** Approving it decides it with no entry; it is then payable.
  - **The batch.** It suggests it and pays it as the item kind `director`, one payment per director, apart from their
    claims. It refuses one that is more than the company owes the person on the payment date (PB-25).
  - **The release.** It records a `director_repayment` (DR-, migrations 000214 and 000215) under
    `director_repayment@1` (LEDGER-45): Dr 2500/2120 naming the person (paid out), Cr the bank, allocated to each
    settlement. The database posts one only with a release linking it (FY254).
  - **Bank import.** It matches the repayment as other money recorded for a batch, by itself when the line names the
    batch.
  - **Rejected by the bank.** `requestDirectorRepaymentVoid` voids it on the approval of another person who holds A
    on "Payment batches", never the person (APR-15); the settlement is open again.
  - **Oracle.** `director_repayment` states the legs.
  - **Not tested yet:** releasing in Standard mode with a director paid; the founder-facing Settle page (4.4).
- [x] 4.4 The Settle a balance page, with the routes, the ledger impact and the approvals. `// req: FR-EQ-05`
  Built: **Settle** on a person's card in Accounting › Director accounts (Owner, Controller) opens
  `/accounting/directors/$contactId/settle`:
  - **Routes.** Repay, keep or waive, starting on `founders.default_route`, which `listDirectorSettlements` now
    answers. Convert and SAFE are shown with when they come.
  - **Details.** The account owed on and the amount; a loan's terms; a waiver's credit account (equity or income,
    never money, a director's account, share capital or GST), capacity and reason.
  - **Ledger impact.** What it posts: plain words, and the lines in Expert mode.
  - **Evidence and approval.** The agreement attached, then Ask for approval, never the person settled, with the
    accountant's review when every approver benefits.
  - **The person's settlements.** Each one opens read-only once asked or decided; a repayment shows its account and
    what batches repaid.
  - **Help.** The help keys and the guide section "Settle a balance".
  - **Canvas.** The screen follows the `director-settlement` fragment; republishing the canvases is task 6.3.

## 5. Conversion and the register of share issues (slice 5)

- [x] 5.1 `RND-SHARE` in `internal/domain/rounding`, with property tests, stated independently in the oracle (D11).
  `// req: FR-EQ-06`
  Built: `rounding.Share` is the largest whole n with n × price < amount + half a minor unit. It has the two pinned
  figures of Appendix G.3, properties (whole, at most the amount, one more share rounds past it, converted plus remainder
  is the amount) and a mutation efficacy of 97%. The oracle's `rnd_share` finds n by search, from the rule, not by the
  formula.
- [x] 5.2 The register of share issues (D11):
  - `share_issue`, append-only, with RLS forced;
  - the source `share_issue` allowed on share capital (INV-13, extended in the database);
  - issues before the books start recorded without an entry.
  Tests: attack tests that no other source posts share capital. `// req: FR-EQ-08`, `// req: INV-13`
  Built:
  - **The table.** `share_issue` (migration 000216), append-only under FORCE RLS and audited. FY255 checks the class is
    share capital, the holder a director or shareholder, and the shares whole.
  - **The backstop.** An entry from the source `share_issue` commits only with the row that names it. A row with an entry
    must be posted on its effective date and credit its class with its amount (FY255, deferred). INV-13's source check
    already allowed `share_issue`.
  - **Before the books start.** `recordShareIssueBeforeBooks` (Owner, Controller, stepped up) records an issue with no
    entry, its amount shares × price (RND-LINE). `listShareIssues` reads the register, the Board viewer included.
  - **Not built.** A cash allotment after the books start, which comes with `p1-equity-register` (FR-EQ-17): cash that
    arrives before the effective date is held as share application monies. The register's page and its guide come with
    5.4.
- [x] 5.3 The convert route (D11):
  - approve with the four documents, the price basis, the capacity and the measurement basis;
  - effective on the date ACRA registered it, posting from the register, refused in a closed period or after the
    approval lapsed.
  The oracle states both bases. `// req: FR-EQ-06`
  Built:
  - **The route.** A settlement converts with the class, the price per share and its basis (the latest priced round
    by default, from `founders.price_basis`; a valuation; or a price every shareholder approves), the capacity
    (lender, employee or shareholder), and the measurement basis. Fair value needs its price, the profit-or-loss
    account and the accountant's note. RND-SHARE gives the shares, the converted amount and the remainder, shown on
    the draft and in the approval's diff with the entry's legs.
  - **The documents.** Asking needs all four, each by its evidence type (FOUNDER-30 names the missing ones). The
    approval records the organisation's day it was approved and posts nothing.
  - **Effective.** `recordConversionEffective` (Owner, Controller, stepped up, never the person) takes ACRA's
    lodgement reference, the resolutions and the effective date. The date is no later than today, no earlier than the
    approval, and no later than the lapse date (FOUNDER-36). It posts `share_issue@1` (LEDGER-46): Dr 2500/2120
    naming the person (converted), Cr the class, and under fair value the difference to the account chosen. The issue
    is recorded with its entry, refused in a closed period. The replay check rebuilds it from the issue and its
    settlement.
  - **The lapse.** `lapses_on` is the date by which the next AGM must be held, six months after the end of the latest
    year ended, or else of the year it falls in [verify]. Editing it with evidence comes with 5.4.
  - **The oracle.** `director_conversion` states both bases with RND-SHARE.
  - **The Settle page.** It takes the conversion's fields, shows the shares and what stays owed, and records it as
    registered.

- [x] 5.4 The ACRA steps (D12) and the register's page on the equity register's screen. `// req: FR-EQ-09`
  Built:
  - **The steps.** `recordConversionStep` records an approved conversion's steps, append-only (migration 000217,
    FY256; a later step supersedes the earlier): the resolution, the approval lodged, the AGM date (when it differs from
    the one worked out [verify]; the lapse and the effective step read it), and the return of allotment. ACRA's
    acknowledgements attach to the approved conversion until it is effective; its files stay append-only.
  - **What they raise.** The settlement answers `acra` with ⏰ approval due 14 days after the resolution (FOUNDER-38);
    ⚠ not registered after `founders.registration_days`, default 14 (FOUNDER-39); and ⚠ within 30 days of the lapse
    (FOUNDER-40). The Settle page shows them and records the steps. The checks page reads them with C.2 #22 in 6.2.
  - **The register's page.** `/equity/transactions` lists every issue and records shares issued before the books start
    (Owner, Controller), linked from Director accounts, with its guide section. Transfers, cash issues, share application
    monies and the cap table come with `p1-equity-register` (FR-EQ-17).

## 6. Presentation, checks and the canvases (slice 6)

- [ ] 6.1 Presentation (D14):
  - net debits under 1250;
  - non-cash labels in the cash flow;
  - shares issued for cash and non-cash apart in the movement in equity;
  - the related-party schedule.
  Tests: each figure checked, and the oracle's statements. `// req: FR-EQ-10`, `// req: FR-EQ-11`
- [x] 6.2 The checks C.2 #21, #22, #23 and #25 on the checks engine (D15), each with a fixture that fires and one
  that passes. `// req: FR-EQ-11`, `// req: FR-EQ-09`, `// req: FR-EQ-12`, `// req: INV-13`
  Built: in the Ledger area.
  - `founders.director-in-debit` (⚠): a net debit at the latest month end, with D9's words.
  - `founders.conversion-registered` (⏰, with ⚠ items): D12's flags of each approved conversion not yet effective.
  - `founders.balances-confirmed` (⚠): balances at the latest year end not confirmed or changed since. It is the
    close's `directors-confirmed`, so the catalogue adds that key no more.
  - `founders.share-capital-registered` (✖): a share-capital account's balance against its class's issues in the
    register.
  `TestTheFoundersChecks` passes each, then fires it.
- [ ] 6.3 The canvases `people-ledger`, `equity`, `workflows`, `sales-purchases` and `banking-claims` rebuilt and
  republished, with the review register's area for the founders' money ([accountant to confirm] readings,
  FR-UX-41).
- [ ] 6.4 `make test` and `make check` green. The build plan and the PRD rows say what was built.

## Workflow follow-up

- Archive the change once every slice has merged (`/opsx:archive p1-founders-money`).
- The SAFE or note route, holders and the cap table follow in `p1-equity-register`.
