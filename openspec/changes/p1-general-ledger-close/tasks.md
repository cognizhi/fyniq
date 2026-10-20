# Tasks

## 1. Periods and the financial year

- [x] 1.1 List periods and open a financial year from the books start and the first year end, as calendar months cut at the year ends; refuse a year more than one past today's, and changes to the books start once a period exists. Tests `// req: FR-ORG-02` and `// req: ledger/posting-rules Posting conventions` (the period of a posting)
  (21 Oct 2026, PRD rev 0.47: `internal/domain/period` works out the years and their months; `GET /v1/periods` lists them a
  page at a time with each one's financial year and the year that opens next (`next_year`, with `may_open`); `GET
  /v1/periods/{id}` with its ETag; `POST /v1/periods:open-year` names the year's first day, so a repeat never opens a second
  year (PERIOD-03), refuses a year more than one past today's (PERIOD-02) or without the years set (PERIOD-01). The profile
  refuses a change to the books start, the start month or the first year end once they are set and a period exists
  (PERIOD-04); setting them for the first time beside earlier periods opens the rest of that year. Rights: V on "Period
  close / reopen" to read, C to open. Tests: `TestYearsAreCalendarMonthsCutAtTheBooksStartAndTheYearEnds`,
  `TestTheNextYearToOpen`, `TestAYearOpensAtMostOneYearAhead`, `TestTheYearsAreFixedOnceAPeriodExists`,
  `TestPeriodsAndFinancialYears`, three authorisation-matrix rows.)
- [x] 1.2 The close checks with their result and the ledger revision they saw; the attestation and close by a second person; the revision check; the close snapshot. Tests `// req: FR-GL-07` (including G4-053 on two connections), `// req: FM-4` and `// req: AUD-04`
  (21 Oct 2026, PRD rev 0.50: migration 000056 adds `period_close_check` and `period_close`, never changed (FY152); the
  database refuses an attestation by the one who ran the checks, on unclean checks or another period's, or before them
  (FY153), and recomputes the snapshot's SHA-256; the API's role may run `balance_differences()`. `internal/domain/closing`
  runs the checks and closes under the ledger lock (`Ledger().Lock`), refusing stale checks (CLOSE-02), unclean ones
  (CLOSE-04), the preparer (CLOSE-05) and an earlier open period (CLOSE-03), and asserts the revision at the close is the
  one the snapshot read. `GET /v1/periods/{id}/close`, `POST …/close-checks` and `POST …/close` (stepped up). Tests:
  `TestThePeriodClose`, `TestAttackTheCloseIsAttestedByAnotherOnItsOwnChecks`, `TestRaceBackDatedPostingAgainstTheAttestedClose`
  (G4-053 both ways), three authorisation-matrix rows.)
- [x] 1.3 Reopening the latest closed period through the approval engine (object type `period`), with a reason and before and after snapshots; posting into a closed period refused by the database. Tests `// req: FR-GL-08`
  (21 Oct 2026, PRD rev 0.51: migration 000057 adds `period_reopen`, never changed (FY154), tied to the period's latest
  close and an approved request to reopen it signed by the one who reopens it (FY155), its after snapshot's SHA-256
  recomputed; the approval engine's request and policy checks take `period` (000058 validates). `closing.Reopenable` and
  `closing.Reopen` (ledger lock first); `POST /v1/periods/{id}/reopen` asks (202 with the request); the envelope holds the
  period's version and status, the close and whether a later period is closed; `getPeriodClose` shows the latest
  reopening and a pending request. Tests: `TestReopeningAClosedPeriod` (own request refused, approval, snapshots, reverse
  order, a request voided by a later close), `TestAttackAReopeningIsTheApprovedRequest`, an authorisation-matrix row.)
- [ ] 1.4 Year-end: retained earnings as the profit and loss of every earlier year, without closing entries; open items carried forward. Test `// req: FR-GL-10` (the optional closing entry waits for question 31)

## 2. Journals

- [x] 2.1 List journals by status, date and text with a cursor; save a draft as a new version under If-Match; record the preparer and the approver. Tests `// req: FR-GL-01`
  (21 Oct 2026, PRD rev 0.48: migration 000052 adds `document_version.created_by` and `document.posted_by`/`posted_at`;
  the database refuses a new version without who saved it and a posting without who posted it and when, and any later
  change to them (FY053, STORAGE-DOC-03). `GET /v1/journals` lists journals newest date first (a draft without a date last)
  by status, dates and text in the memo or number, with debit totals per currency; `PUT /v1/journals/{id}` saves a draft's
  next version under If-Match; every journal answer names its preparer and poster. The approver becomes the approval's
  signers with task 2.2. Tests: `TestJournalListDraftVersionsAndWhoPrepared`, `TestAttackDocumentActorsAreRecordedOnce`,
  two authorisation-matrix rows.)
- [x] 2.2 Amount conditions on a `journal` approval policy: FM-2's self-approval at or below the threshold, flagged and logged for the FM-3 review; above it, an approval request; SOD-3 in Standard mode; FM-1 for voids. Tests `// req: FR-GL-01`, `// req: FM-2`, `// req: SOD-3` and `// req: FM-1`
  (21 Oct 2026, PRD rev 0.53: the threshold is a setting, `approvals.self_approval_threshold` (S$2,000.00, dual
  control), not a condition on the policy: policy conditions wait for wave 2's documents (FR-APR-01). Migration 000059
  lets approval requests and policies take `journal` with `journal.post` and `journal.reverse`, and the review log
  take `self_approved` (000060 validates). `postJournal` refuses above the threshold in every team mode (JOURNAL-07);
  `requestJournalPost` asks; the approval checks SOD-3 against who saved the version (JOURNAL-09) and posts as the
  approver. In Founder mode `reverseJournal` refuses a posted journal (JOURNAL-08) and `requestJournalReversal` asks.
  Tests: `TestJournalApprovals`, `TestJournalThresholdInStandardMode`, the matrix rows. The web screens follow.)
- [x] 2.3 Edit a posted journal in an open period: a new version whose post writes the reversal and the replacement in one transaction. Tests `// req: FR-GL-12` and `// req: ledger/posting-rules Edits of posted documents`
  (21 Oct 2026, PRD rev 0.54: `editJournal` takes the new content and a reason; `journal.Service.Edit` saves the next
  version, reverses the old entry on its own date and posts the new one, in one transaction; the journal stays posted.
  Migration 000061: `document_guard` lets a posted document's version move only with who posted the edit set anew
  (FY053) and freezes reversed documents (FY055); the deferred `document_edit_posted` checks both postings at commit
  (FY055); approval requests take `journal.edit` (000062 validates). In Founder mode an edit is refused (JOURNAL-10),
  and above the threshold in any mode (JOURNAL-07); `requestJournalEdit` keeps the content beside the envelope, bound
  by its SHA-256, and lists the changed lines. Tests: `TestEditAPostedJournal`, `TestEditAPostedJournalInFounderMode`,
  `TestAttackAnEditReversesAndPostsTogether`, the matrix rows. The oracle's edit shape stays with 8.1; the web
  follows.)
- [ ] 2.4 Auto-reversing journals (the reversal date, by default the first day of the next period) and recurring journal templates. Tests `// req: FR-GL-02`
  (21 Oct 2026, part, PRD rev 0.55: a journal carries `reverse_on`, after its date (JOURNAL-11); the scheduler's hourly
  `journals.auto-reverse` reverses it on that date as `scheduler:journals.auto-reverse` (5.1). Tests:
  `TestAutomaticReversals`, `TestAJournalReversesByItselfOnItsDate`. **Left:** recurring journal templates; the
  editor's reversal date.)

## 3. Opening balances

- [x] 3.1 One opening-balance document per organisation at the books start, in base currency, posting only when it nets to zero; INV-13 as an allow list (`opening_balance`, `share_issue`) in a new migration. Tests `// req: FR-GL-06`, `// req: ledger/posting-rules Opening balances` and a database attack test `// req: INV-13`
  (21 Oct 2026, PRD rev 0.49: migration 000054 adds the `opening_balance` document type and the `opening_balance` table
  keyed on the organisation (one per organisation; FY054 keeps it of its type and unchanged) and makes INV-13's
  `journal_line_check` an allow list; 000055 validates the type check. `ledger.PostOpening` posts as entered under
  `opening_balance@1`, share capital allowed, base currency only (LEDGER-17). `GET/PUT /v1/opening-balances` (ETag
  `"0-none"` before the first save) and `POST /v1/opening-balances/post`. Tests: `TestOpeningBalancesPostAsEnteredWithShareCapital`,
  `TestOpeningBalancesNetToZeroInTheBaseCurrency`, `TestOpeningBalances`, `TestAttackOpeningBalancesAreOneAndOnlyTheyReachShareCapital`,
  the allow list in `TestAttackManualJournalToShareCapitalFails`, three authorisation-matrix rows. **Left:** the
  oracle's side (8.1); changes after posting with FM-1 (2.3); the screen (7.2).)

## 4. The general ledger report

- [x] 4.1 Lines of any accounts and dates in base currency with opening, running and closing balances, at most 5,000 lines per account per request, and the source of each line. Tests `// req: FR-GL-11`
  (21 Oct 2026, PRD rev 0.52: `balance.GeneralLedger` (the balance service, ENG-04) reads the opening from the cached
  balances of earlier periods and the earlier lines of the first day's period, then the account's lines in posting order;
  `GET /v1/reports/general-ledger?account_id=…&from=…&to=…` (one to 20 accounts, each its own parameter) with the source
  of each line; REPORT-01 beyond 5,000 lines; policies may need a right without a qualifier (`Policy.Unqualified`),
  which the authorisation matrix reads. Tests: `TestTheGeneralLedger`, an authorisation-matrix row. **Left:** the export
  and the screen (7.3).)

## 5. Scheduler and system postings

- [ ] 5.1 The scheduler tick: due items once per slot, under the `system:scheduler` principal, period locks and approval policies applied. Tests `// req: FR-AUT-01` and `// req: FR-AUT-02`
  (21 Oct 2026, part, PRD rev 0.55: the tick's platform schedules gain `journals.auto-reverse`, hourly at :20, run with
  the worker's role (migration 000063: UPDATE (status) on `document`, and `document_reversed_posted`, FY056, so any
  reversal needs its entry); each run names `scheduler:journals.auto-reverse`; a reversal whose period is closed or not
  open waits with `journal.reversal_waiting`. The principal is per job rather than one `system:scheduler`, so the audit
  trail says which job acted. **Left:** recurring templates, and the other automated postings with their sources.)
- [ ] 5.2 Recognition up to the current period only, with future amounts as projections. Test `// req: ENG-05`
- [ ] 5.3 Versioned posting rules with a replay test per rule version. Test `// req: ENG-06` (QA-ENG-06)

## 6. Validation, framework and schedules

- [ ] 6.1 The journal's ported workbook statuses with their severities and messages. Tests `// req: ENG-20`, `// req: ENG-21` and `// req: ENG-23`
- [ ] 6.2 The accounting framework and edition from a financial year, the accountant's confirmation and the policies with their approver and date. Tests `// req: FR-ORG-10`
- [ ] 6.3 Month-on-month flux on the close: variances above the threshold need an explanation before the close is signed off. Test `// req: FR-GL-14` (the budget half waits for `p1-planning`)
- [ ] 6.4 Schedules for prepayments and deferred revenue, with the first source that spreads over months (`p1-purchases` bills). Tests `// req: FR-GL-03`

## 7. Screens

- [ ] 7.1 Build journals, the journal editor and the journal record page as designed. Add help keys, tooltips and `docs/user-guide/` pages compiled into `kb/`
  (21 Oct 2026, part: Accounting › Journals lists journals newest first with status filters and search, debit totals,
  preparer and poster; the journal page prepares a new journal or changes a draft (lines with debit or credit, currency
  and rate, the balance per currency added up exactly as typed), checks it with a dry run that shows the entry in the
  base currency, posts it, and reverses a posted journal with a date and a reason; a refusal for want of a period links
  to Periods and close. A new Accounting page lists the workspace's pages. Guide `journals.md` → kb `accounting.journals`.
  Approvals (2.2, rev 0.53): a post refused above the self-approval threshold offers **Ask for approval**; in Founder
  mode **Ask to reverse** asks for the reversal with its date and reason; a journal with a request waiting says so and
  links to it, and the approval page links back to the journal. Guide `journals.md` and `approvals.md` → kb
  `accounting.journals` and `approvals.decide`.
  **Left:** the Recurring tab and auto-reversal (2.4), evidence on the editor, dimensions (`p1-organisation-chart`
  3.3), the locked-period banner (1.2).)
- [ ] 7.2 Build opening-balances and period-close as designed, with the periods of the year on the close page
  (21 Oct 2026, part: Accounting › Periods and close lists the periods by financial year, open or closed, and opens the
  next year; Home's set-up checklist gains "Open the first financial year". Guide `periods-and-close.md` → kb
  `accounting.periods`. Accounting › Opening balances lists every balance-sheet account (revenue and expenses on request)
  with a debit and a credit, the total adding up exactly as typed; it saves a draft at the version read, checks and posts
  once; Home's checklist gains "Enter the opening balances". Guide `opening-balances.md` → kb
  `accounting.opening-balances`. Each month opens its close page: an Accountant or a Controller runs the checks, which
  show what they found and when they go stale; another founder, an Owner or a Controller who did not run them, attests
  with a statement and closes (step-up through "Confirm it's you"); a closed month shows who attested it, the revision
  and the snapshot's hash; an Accountant or a Controller asks to reopen the latest closed month with a reason, the
  pending request shows with a link to it, and the latest reopening shows its snapshots. **Left:** the checklist template
  and reconciliations, flux (6.3), year end
  (1.4), open items (with `p1-sales`, `p1-purchases`), changes after posting (2.3).)
- [x] 7.3 Build the general ledger report (its screen belongs to `p1-reports`; built early so the founders see what they post)
  (21 Oct 2026: Accounting › General ledger (`/reports/general-ledger`): accounts chosen one at a time (up to 20), a
  range of dates from the first of the month to today, each account's opening balance, lines with the running balance
  and their source as a link (a journal, the opening balances), debits, credits and closing balance, the ledger revision
  it read; Export CSV with plain decimals, an ASCII minus and inert formulas. Guide `general-ledger.md` → kb
  `reports.general-ledger`. **Left:** dimensions and saved views, the background export (`p1-reports`).)
- [ ] 7.4 Build schedules with 6.4

## 8. The oracle (for the oracle session; never written from the engine)

- [ ] 8.1 Opening balances, auto-reversals and the reversal and replacement of an edited journal as scenario shapes, generator kinds and golden cases, written from the PRD rules by an author who has not read the engine (QA-ENG-01)

## 9. Finish

- [ ] 9.1 Every new operation in the contract (`make openapi`), with its authorisation-matrix rows
- [ ] 9.2 Finish with `make check`, `make test` and `make test-e2e`
