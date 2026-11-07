# oracle

The independent reference ledger (QA-ENG-01): a second implementation of the posting rules in
Python, written from PRD-001 §10 and Appendix B, never translated from the Go engine. The
differential tests require the engine and the oracle to agree to the cent (QA-ENG-02, golden
rule 3). Whoever writes or changes it must not read the engine's code (`internal/domain/`,
`internal/adapters/postgres/`, `cmd/scenario-run/`): a rule is changed here from the PRD, and a
disagreement is settled by the PRD, never by copying the other side.

## What it computes (P0)

P0 posts manual journals and corrects them by reversal, so that is what the oracle implements
(`src/oracle/ledger.py`, whose docstring lists the rules):

- **`RND-FX`** (§10.4): base amount = round(amount × rate, the **base** currency's minor units),
  half away from zero. It is the one rounding a manual journal needs, and it lives in
  `src/oracle/money.py` under its policy name.
- **The rules a journal meets to post**: amounts and rates at the input precision boundary (ENG-44);
  at least two lines; no zero line; active, non-group accounts; a rate and its source on every line
  not in the base currency; balanced in the base currency (and in its own currency when every line
  shares one). Anything else is rejected whole.
- **Reversals**: the negation of every stored line, on the reversal's date; at most once per document.
- **The trial balance by month**: the base-currency total per account per posting month.

## What it computes (P1, pending the engine's side)

The P1 sources' rule sets, each written from PRD-001 §10.2, §10.4 and Appendix B in a session that
never read the engine, post through the same ledger (`src/oracle/sources/`): bank lines and their
spreads, and transfers between money accounts (`bank_transaction@1`, `bank_transfer@1`); bills, vendor
credits, payments of bills and bills' spreads (`bill@1`, `vendor_credit@1`, `bill_payment@1`); invoices,
credit notes, customer receipts, deposits, deposit refunds, write-offs and invoices' spreads
(`invoice@1`, `credit_note@1`, `customer_receipt@1`, `customer_deposit@1`, `deposit_refund@1`,
`write_off@1`); expense claims, their reimbursements and cash advances (`expense_claim@1`,
`claim_reimbursement@1`, `cash_advance@1`, `RND-MILEAGE`); the fixed asset register's depreciation and
disposals (`asset_depreciation@1`, `asset_disposal@1`); and the general ledger's opening balances, journals that reverse
by themselves and edits of posted journals (FR-GL-02, FR-GL-06, FR-GL-12, task 8.1 of `p1-general-ledger-close`); and the
month-end revaluation of foreign-currency monetary items with its reversal, and `RND-RATE`'s quotes (FR-GL-04, INV-05,
task 6.1 of `p1-multi-currency-fx`, `src/oracle/revaluation.py`, whose golden case waits in `test/differential/pending/`); and
dimensions (FR-DIM-01, FR-DIM-02, PRD-001 rev 0.171 and 0.172): the values a journal's or the opening balances' line
names, what an account requires of the lines a person enters, and each month's balances by account and set of values
(`golden/dimensions.json`, its figures worked by hand in `tests/test_dimensions.py`); and account merges
(`account_merge@1`, FR-COA-04, Appendix B.8, PRD-001 rev 0.174): what an account holds in each currency and dimension set
(an income or expense account in the financial year of `settings.books_start` and `settings.first_year_end`), moved at its
recorded base value with the base `RND-RATE` leaves in the base currency, and the account inactive afterwards
(`src/oracle/sources/merge.py`; `golden/account-merge.json`, its figures worked by hand in `tests/test_merge.py`); and the
equity register: cash share issues, transfers and the cap table at any day (`share_issue_cash@1`, FR-EQ-17, FR-EQ-20,
`src/oracle/captable.py`), and SAFEs, their money on 2700 keyed by instrument and their conversion at a priced round
(`instrument_conversion@1`, FR-EQ-18, `RND-SHARE`, G4-044; `src/oracle/sources/equity.py`; `golden/safe-conversion.json`
and `golden/safe-conversion-caps.json`, their figures worked by hand in `tests/test_safes.py`). Their
scenario shapes, the canonical form of their entries and the readings where the PRD leaves a shape
open are in [`test/differential/README.md`](../test/differential/README.md), "P1 sources". The
engine's `cmd/scenario-run` does not read them yet, so their golden cases wait in
`test/differential/pending/` with their figures worked out by hand.

Money is `decimal.Decimal` with explicit contexts only, and there is no binary floating point
anywhere (ENG-10; a test fails on the word). `money.EXACT` traps every rounding, so an arithmetic
step either is exact or raises; the named policies are the only code that rounds.

## Use

```bash
make oracle-test                                                      # its own tests (part of make test)
uv run --project oracle python -m oracle run scenario.json            # the result JSON on stdout
uv run --project oracle python -m oracle run scenario.json --explain  # + why each rejection, on stderr
uv run --project oracle python -m oracle generate --seed 1 --count 500 --out .data/scenarios
ORACLE_HYPOTHESIS_PROFILE=thorough make oracle-test                   # 2,000 examples per property
```

The scenario and result formats, and the rules a comparator relies on, are in
[`test/differential/README.md`](../test/differential/README.md).

| Path | Holds |
|---|---|
| `src/oracle/money.py` | Parsing at the precision boundary; the named rounding policies (`RND-FX`, `RND-GST-EXCL`, `RND-GST-INCL`, `ALLOC-EVEN`, `RND-LINE`, `RND-GST-SHARE`, `RND-WHT`, `RND-DISCOUNT`, `RND-DEPOSIT`, `RND-MILEAGE`, `RND-RATE` of a payment and of a quote); exact sums and formatting |
| `src/oracle/scenario.py` | Reading and checking `fyniq-scenario@1`; checking `fyniq-scenario-result@1` |
| `src/oracle/ledger.py` | The manual journal's rules (rule 11 on dimensions included), reversals and edits, the opening balances, the P1 sources' dispatch and schedules (automatic reversals included), the trial balance and the balances by dimension set |
| `src/oracle/revaluation.py` | The month-end revaluation (FR-GL-04): the open items on the accounts marked revalue, document by document on 1100 and 2010, their base-only lines against 7040, and the reversal on the next month's first day |
| `src/oracle/book.py` | Lines (with their dimension values), entries, scheduled entries, rejections and the canonical form of a P1 entry |
| `src/oracle/rules.py` | What every P1 rule set checks the same way: the boundary, rates, accounts (and what a line a person enters on one requires), GST, spreads |
| `src/oracle/sources/` | One rule set per P1 source (`bank.py`, `purchases.py`, `sales.py`, `claims.py`, `assets.py`, `founders.py`, `merge.py`, `equity.py`: cash issues, transfers, SAFEs and their conversion) and the settlement of posted documents by money (`settle.py`) |
| `src/oracle/strategies.py` | Hypothesis strategies that generate manual-journal scenarios (task 2.4), a quarter of them declaring dimensions |
| `src/oracle/strategies_p1.py` | Hypothesis strategies for the P1 sources, with each document's intent; the `fx` source marks accounts revalue and draws period-end rates |
| `src/oracle/strategies_gl.py` | The general ledger's source of `strategies_p1`: opening balances, journals that reverse by themselves, voids and edits, each built to an intent; a third of its scenarios declare dimensions |
| `tests/` | Hand-worked journals, rejections and P1 documents (`# req: QA-ENG-01`), the pending golden cases, the format, properties, the CLI |

Python and uv are pinned in the repository's `mise.toml`; the dependencies are pinned in
`pyproject.toml` and `uv.lock` to releases at least two weeks old (`exclude-newer`). Update the lock
with the pinned uv: `uv lock --project oracle`.

## Credit

The ledger is a port of the workbook's reference ledger, `tests/reference.py` in the
`zh-sheet-accounting` repository (the Zoho Sheet workbook that Fyniq replaces, by the same owner),
used here under this repository's MIT licence. The port keeps its shape (postings accumulated per
account and month, read back as the trial balance) and replaces its binary-float rounding with the
named decimal policies of PRD-001 §10.4.
