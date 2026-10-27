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
`claim_reimbursement@1`, `cash_advance@1`, `RND-MILEAGE`). Their
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
| `src/oracle/money.py` | Parsing at the precision boundary; the named rounding policies (`RND-FX`, `RND-GST-EXCL`, `RND-GST-INCL`, `ALLOC-EVEN`, `RND-LINE`, `RND-GST-SHARE`, `RND-WHT`, `RND-DISCOUNT`, `RND-DEPOSIT`, `RND-MILEAGE`, `RND-RATE` of a payment); exact sums and formatting |
| `src/oracle/scenario.py` | Reading and checking `fyniq-scenario@1`; checking `fyniq-scenario-result@1` |
| `src/oracle/ledger.py` | The manual journal's rules, reversals, the P1 sources' dispatch and schedules, the trial balance |
| `src/oracle/book.py` | Lines, entries, scheduled entries, rejections and the canonical form of a P1 entry |
| `src/oracle/rules.py` | What every P1 rule set checks the same way: the boundary, rates, accounts, GST, spreads |
| `src/oracle/sources/` | One rule set per P1 source (`bank.py`, `purchases.py`, `sales.py`, `claims.py`, ...) and the settlement of posted documents by money (`settle.py`) |
| `src/oracle/strategies.py` | Hypothesis strategies that generate manual-journal scenarios (task 2.4) |
| `src/oracle/strategies_p1.py` | Hypothesis strategies for the P1 sources, with each document's intent |
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
