# The workbook's golden data sets (QA-ENG-07)

The workbook Fyniq replaces, `zh-sheet-accounting`, tests its engine on data sets of its own (PRD-001 Appendix G.1).
They are ported here as fixtures with the figures its tests pin, and the Go engine and the independent oracle must both
reproduce every figure (golden rule 3; `p1-launch-readiness` task 3.1, design D4). A disagreement is a bug, fixed through
the bug-fix skill, never by editing an expected value.

| File | What | Written by |
|---|---|---|
| `sources.json` | The workbook's repository and the commit read, the SHA-256 of each source file and of each fixture | `scripts/workbook_golden.py` |
| `<set>/scenario.json` | The set as one scenario, `fyniq-scenario@1` ([the format](../../differential/README.md)) | `scripts/workbook_golden.py` |
| `<set>/mapping.json` | Every row of the workbook's set: the document it became, or why Fyniq does not post it (`not_ported`), and how Fyniq reads it where that differs from what was typed (`read`) | `scripts/workbook_golden.py` |
| `<set>/expected.json` | `fyniq-workbook-golden@1`: the documents both sides refuse, and each figure the workbook's tests pin, naming the test | A person, from the workbook's tests |

The script reads the workbook's Python files as data, evaluating only literals, dates and the rows' calls, so nothing
from that repository runs (D4; the data is synthetic, DEV-08). Run it with a checkout at the commit to pin:

```
python3 -I scripts/workbook_golden.py <path to zh-sheet-accounting>
```

Then review the diff. `expected.json` changes only by hand, from the workbook's tests; the script records its new hash.

## Tests

- `TestTheWorkbookGoldenSets` (`internal/differential`, local flavour, `make test`): each set posts through the engine
  and the oracle, which must agree to the cent (`Compare`), refuse exactly the documents `expected.json` lists, and both
  reproduce every figure it pins.
- `TestTheFixturesAreTheRecordedOnes`: every fixture is the one `sources.json` records, so no figure changes quietly,
  and every document `mapping.json` names is in its scenario.
- `scripts/test_workbook_golden.py` (`make script-tests`): the reader reads rows and dates, and refuses code.

## The sets

| Set (Appendix G.1) | Workbook files | Here |
|---|---|---|
| Posting edge cases | `tests/edge_posting.py`, `tests/test_edge_posting.py`, `src/cfh/coa.py` | `posting/`: 71 figures, 8 documents refused |
| Sample company | `src/cfh/sample.py`, `tests/reference.py`, `tests/ref_*.py` | Reconciled by the migration (QA-MIG-01): `TestTheReconciliationComparesTheBooksWithTheWorkbook` compares 1,649 figures of the imported sample with the workbook's own engine, each account's opening, movement and closing of every month. The oracle checks each rule the sample posts by through the differential suite; the sample is not one oracle scenario, since it holds pay runs, which the format has not (the owner's decision of 16 Nov 2026) |
| Non-posting edge cases | `tests/edge_nonposting.py`, `tests/test_edge_nonposting.py` | `non-posting/`: its 125 cases (`statuses.json`), each block mapped to the Fyniq rule and tests that hold it (`mapping.json`, `TestTheNonPostingCasesHaveTheirTests`) |
| Document numbers | `tests/test_edge_docnumbers.py` | `document-numbers/`: 18 figures, 2 journals refused; and the bank matcher's `TestAWorkbookNumberNamesOnlyItself` |
| Regressions | `tests/test_regressions.py` | `regressions/` (its two fixtures in one scenario): 9 figures; and `TestAWorkbookNumberNamesOnlyItself` |
| Miscellaneous | `tests/test_edge_misc.py` | `miscellaneous/`: 7 figures, the unrealised FX among them; each of its tests held (`functions.json`, `cases.json`) |

## The posting edge cases: how Fyniq reads them

The set has one row per status branch of the workbook's posting tables. Its figures (`tests/test_edge_posting.py`):
document totals, Doc Lines amounts, what stays owed, 7030 in each month, the spreads of 4010, 6450 and 6210, each
asset's depreciation and 6610, and July's GST. Each is in `expected.json` with its test's name; all hold in Fyniq. Where
Fyniq reads a row otherwise, `mapping.json` says so:

- **✖ rows never post.** The eight the format can hold (an unknown bank, category or account, a bank line to itself, an
  unbalanced journal, a residual above cost) are refused by both sides. The rest are inputs the API refuses before
  anything posts (no date, an unreadable one, no amount, both in and out, no account; ENG-23, ENG-44). Two documents
  cannot share a number: Fyniq numbers each from its sequence (FR-ORG-04).
- **Settlements.** Realised FX comes first (ENG-41), then a residual within tolerance (FR-BNK-08, the workbook's rule):
  INV-E20's 20.00 and SB-E21's 120.00 are realised FX, INV-E28's 0.60 a residual settled in full. The figures on 7030
  are the workbook's.
- **The opening invoice** INV-E24 is an opening item (rev 0.257), posted at the books start against 3900 Opening
  balance clearing, a Fyniq account the workbook does not have; the opening balances carry 3900 where the workbook's
  carry 1100.
- **GST registration** is the workbook's "Yes" with no date (`S_GSTStart` starts the return's period), and the opening
  invoice bears GST: registered throughout. The F5 boxes of `test_gst_return` wait for F5 (`p1-gst`, at registration);
  July's figures are checked on the accounts they post to, 2200 among them.
- **Rows dated before the books start** do not post: Fyniq has no period then. What came before is in the opening
  balances, or an opening item (FR-GL-06).
- **A bank line on 1100 or 2010** whose reference names no document waits for its match (FR-BNK-07). A document with
  no customer or supplier is refused (FR-AR-01, FR-AP-01). A draft posts nothing in either.
- **Spreads** are offered only on income and expense accounts (FR-GL-03): the laptop's bank line and SB-E23 post
  unspread, as the workbook does in effect.
- **A document's total is its lines'.** INV-E14 posts its 4,500 of lines, not the 5,000 typed; INV-E26 leaves out its
  line of quantity 0, which the API refuses, for the same total; INV-E31's SGD item price is converted at its rate.
- **INV-E32** takes the average of August's two table rates, 1.34, as Fyniq's rate cascade does (ENG-40).
- **The old phone's disposal** posts, a write-off with no proceeds, where the workbook asks for a journal; its
  depreciation stops in June as the workbook's does. The disposal account is the workbook's 7040.
- **The account the workbook deactivates** (6450, while its spread still releases) stays active here: Fyniq refuses a
  posting to an inactive account. The workbook's warning is the non-posting set's.
- **The server bought before the books start** depreciates from January in the ledger; its 3,200 accumulated by
  September includes the 1,400 of June to December 2025, which the asset register opens with (FR-FA-01). The format
  has no register opening, so the check is the 1,800 posted.
- **Not figures.** The status column and the Checks sheet's counts (`test_every_status_branch`,
  `test_quote_and_account_statuses`, `test_books_balance_and_checks_count_the_rows`) are the workbook's way of saying
  what posted; Fyniq refuses what does not. `test_error_rows_never_post` compares the trial balance with the workbook's
  reference ledger, which is its Python and is not run (D4); the engine and the oracle are compared on the whole result
  instead.
- **Payroll** (`PINNED_PAY`): pay runs (FR-PAY-01 to FR-PAY-03) are not in the launch; these cases come with them.

## The document numbers and the regressions

The workbook found its numbers with look-up functions that read `*`, `?`, `~` and a leading `<` as patterns or
comparisons, and missed a reference with spaces around it. Fyniq names a document by its ID, and its bank matcher
(`internal/domain/banking/suggest.go`) compares numbers as text, on word boundaries, trimmed and normalised:
`TestAWorkbookNumberNamesOnlyItself` holds each of the workbook's numbers and references. The scenarios hold the
settlements those references make. Their IDs spell out what the format's IDs do not take (`INV-9*` is `INV-9-star`,
`<B1>` is `lt-B1-gt`; each said in `mapping.json`).

- A reference that names no document (`INV-9?`) waits for its match, as the workbook flags it; one of only spaces, or
  none, with its contact named, is recorded on account for that contact (FR-AR-18), which is how the workbook counts
  it.
- A journal has one date in Fyniq (FR-GL-01), so the workbook's entry with two (`J-1`) cannot be entered; the
  unbalanced ones (`J*`, `J1`) are refused by both sides.
- Not figures: quotes' lines, the print sheet and timesheets (`test_quotes_lines_print_and_timesheets`) post nothing;
  projects and timesheets are not in the launch.

## The miscellaneous cases

Ported as figures: the unrealised FX of 240 − 30 = 210 at the latest monthly rate, which Fyniq posts as a revaluation
at each month's end and reverses on the next day (FR-GL-04; Appendix G.3), on its own unrealised FX account (7045 here:
the workbook's 7040 is its disposal account), the scenario running to 1 October to show September's reversed; the
customer's statement balance of 372 (an invoice, a credit note and a payment not matched to an invoice); and the
transfer from savings, recorded once.

Its other tests, each held (`cases.json`, checked by `TestTheNonPostingCasesHaveTheirTests`):

- The bank import seeing a transfer recorded from the other account: `TestATransfersOtherSideIsRecognised`.
- The reminder in the invoice's currency: a reminder emails the invoice's own PDF, in its currency
  (`TestRemindersWaitForAPerson`); the carry-forward of foreign items in their currency: a clean start's opening items
  (`TestACleanStartCarriesTheBalancesForward`, `TestAnOpeningItemPostsItsTotalAgainstTheClearingAccount`).
- The budget of discounts: Fyniq budgets as the P&L shows it, so the migration negates the workbook's positive discount
  budget (`TestADiscountBudgetReducesRevenue`); the overdraft by month-end: the close's check C.1 #31
  (`TestEachCloseBackedCheckReadsItsOwnFindings`).
- A feasibility study without an IRR: `TestTheWorkbooksStudyWithoutAnInvestment`. The averages of the books' last three
  months are hints of the workbook's sheets; Fyniq's study takes the figures typed for it.
- The print sheet's blank dates: Fyniq renders each document's PDF from the document.
- The hourly cost and timesheets: with projects and pay runs, after the launch (rev 0.146).

## The non-posting edge cases

The workbook checks each row of its tables as it is typed and shows a status beside it; Fyniq checks a record whole
before it is saved and refuses it in plain words (ENG-23). `statuses.json` lists the workbook's 125 cases; `mapping.json`
gives each block of them the Fyniq rule and the tests that hold it, and where Fyniq reads a status otherwise: an
account's code is letters and digits, not four digits; a report group is the account's own text; nothing posts to an
inactive account; a document's number comes from its sequence; a line belongs to its document. Employees, projects and
timesheets wait for pay runs and projects, after the launch. The workbook's print sheet has no counterpart.
