# Differential scenarios (QA-ENG-02)

The Go engine and the independent Python oracle (`oracle/`) each read the same **scenario**, post
it, and write a **result**; a comparator diffs the two results to the cent (golden rule 3). This
folder holds the contract between them (OpenSpec change `p0-python-oracle-differential`, task 2.1):

| File | What |
|---|---|
| `scenario.schema.json` | `fyniq-scenario@1`: what both sides read |
| `result.schema.json` | `fyniq-scenario-result@1`: what both sides write |
| this README | The rules a JSON Schema cannot state, and how to compare two results |
| `golden/` | The golden scenarios the Go gate posts through both sides |
| `pending/` | Golden scenarios of P1 sources, waiting for `cmd/scenario-run` to read them |

Both schemas are JSON Schema draft 2020-12 with RE2- and ECMA-262-compatible patterns, so a Go
validator and Python's `jsonschema` accept the same documents. The oracle checks its input against
the scenario schema and its output against the result schema. Still to come in the same change: the
Go side of the round trip (2.1), `cmd/scenario-run` (2.2), the comparator (2.3), golden data sets
from `cmd/seed` (2.4) and the merge gate (3.1).

## Scenario

```json
{
  "schema": "fyniq-scenario@1",
  "name": "short-name",
  "base_currency": "SGD",
  "currencies": {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3},
  "accounts": [{"code": "1010", "type": "asset", "group": false, "active": true},
               {"code": "4010", "type": "revenue", "group": false, "active": true}],
  "documents": [
    {"type": "manual_journal", "id": "JNL-0001", "version": 1, "date": "2026-01-05", "memo": "",
     "lines": [
       {"account": "1010", "currency": "USD", "amount": "100.00", "fx_rate": "1.3472", "fx_rate_source": "document"},
       {"account": "4010", "currency": "SGD", "amount": "-134.72"}
     ]}
  ],
  "reversals": [{"document": "JNL-0001", "date": "2026-01-06"}]
}
```

- Account types: asset, liability, equity, revenue, expense. Amounts are signed decimal strings
  (debit positive, credit negative) at most at the currency's minor units; `fx_rate` (at most 10
  decimals, positive) and `fx_rate_source` are present only on lines not in the base currency, and
  a rate always comes with its source (PRD-001 rev 0.24).
- An account may carry a `role`. `share-capital` marks the accounts a manual journal may not post to
  (INV-13; 3010 and 3020 in the default chart). Both sides refuse those lines (change
  `p0-manual-journal-rules`); half of the generated charts have such an account. The statements read
  three more roles, `retained-earnings`, `depreciation` and `income-tax-expense`, and the attributes
  `report_group`, `function` and `cash_flow_line` (section "Statements"). (PRD-001 rev 0.167)
  `director-loan` (2500) and `deferred-remuneration` (2120) mark the director's accounts: each line on
  one names its `party`, one of the scenario's `people`, and its `origin` (rule 9; FR-EQ-01, B.7). The
  generator gives half of its charts one, with the people `P0001` and `P0002`. (PRD-001 rev 0.207)
  `share-application-monies` (2040) marks the account holding money received for shares not yet allotted: each line
  on it names its subscriber, one of `people` or `subscribers`, the investors not among the people (B.7, FR-EQ-17).
  `convertible-instruments` (2700) marks the account holding each SAFE's money under its instrument: each line on it
  names a recorded instrument, and no other line names one (FR-EQ-18; section "SAFEs and their conversion").
- Documents post in array order; then each reversal reverses the posted entry of its document on
  its date. Every date falls in a calendar-month period, and all periods are open.
- (PRD-001 rev 0.49 to 0.66) `reversals` holds the corrections of posted journals in the order they are made: a
  reversal (a void, `{document, date}`) or an edit (`{document, edit}`, the next version); a manual journal may
  carry `reverse_on`; and a document may be the opening balances. Section "Opening balances, automatic reversals
  and edits" below.

### Invalid scenario or rejected document

There are two kinds of failure, and they must not be confused:

**The scenario is invalid** (the oracle exits with status 2 and writes nothing to stdout; the engine's
runner should fail the same way). The input is not a `fyniq-scenario@1` document:

- it breaks `scenario.schema.json` (a missing or extra key, a wrong type, a JSON number where a string
  is required, a document type the format does not know, more than 4 minor units, an ID with `/`,
  an account role other than `share-capital`, or an `fx_rate` without its `fx_rate_source`);
- a name, ID, account code or currency code does not match its pattern as a whole string (Python's
  `re` lets `$` match before a final newline, so the oracle checks `"JNL-0001\n"` again; RE2 refuses it);
- a JSON object repeats a key, or the JSON holds `NaN` or `Infinity`;
- `base_currency` is not a key of `currencies`;
- two accounts share a code, or two documents share an ID;
- a date is not a real calendar date (`2026-02-30`);
- a P1 rule of the format is broken (section "P1 sources" below: no `settings` or `as_of`, an `as_of`
  before a date in the scenario, a reversal of a P1 document, a settings account not in `accounts`);
- a rule of the general ledger's shapes is broken (section "Opening balances, automatic reversals and edits"): a
  second `opening_balances`, opening balances without `settings.books_start` or dated another day, opening
  balances with `statements`, a `reverse_on` without `as_of`, a reversal of the opening balances, an edit of a
  P1 document, or an edit of the opening balances dated another day or with `reverse_on`;
- two accounts have the role `retained-earnings`, `depreciation` or `income-tax-expense`, or one of
  them is on an account of the wrong type (equity, expense and expense), or `statements` is true with no
  `retained-earnings` account (section "Statements"), or `kpis` comes without `statements` (section "KPIs");
- a rule of the month-end revaluation is broken (section "Month-end revaluation"): `revalue` on an account that is not
  an asset or a liability or on the prepayments or deferred revenue account, a flagged account without `as_of` or
  without `settings.accounts.unrealised_fx` naming an active revenue or expense account that is not a group and has no
  role, a flagged account in a scenario with `statements`, or a period-end rate dated other than a month's last day, in
  the base currency or one not in `currencies`, a second for its currency and date, or whose rate or quote is not a
  valid rate (a quote's stored rate included);
- a rule of dimensions is broken (section "Dimensions"): two dimensions share a name or a dimension two values a code,
  an account `requires` a name not in `dimensions`, or a line's `dimensions` names a dimension not in `dimensions` or
  a code not among its values;
- `votes_per_share` is on an account without the role `share-capital` (section "Cap tables").

**A document is rejected** (it is listed in `rejected`, and none of its lines posts). A journal posts
only when it meets every rule:

1. Each line's currency is a key of `currencies`; each amount is a plain decimal string
   (`-?[0-9]+(\.[0-9]+)?`: no exponent, `+`, grouping, spaces, `NaN` or non-ASCII digits) with at most
   the currency's minor units, written decimals counted (`"12345.0"` is refused for JPY; `"100"` is
   fine for USD) and fewer than 17 integer digits; each rate is a plain decimal string, positive,
   with at most 10 decimals and 10 integer digits (ENG-44, ENG-11, §11.3). Nothing is ever rounded to
   make it fit.
2. At least two lines.
3. No line is zero (in its own currency).
4. Every account exists, is active and is not a group account (INV-04), and none has the role
   `share-capital` (INV-13).
5. Every line not in the base currency has `fx_rate` (and so its source); a base-currency line has
   neither `fx_rate` nor `fx_rate_source` (ENG-03).
6. Every base amount has fewer than 17 integer digits (NUMERIC(20,4)).
7. The base amounts, each computed by `RND-FX` (round(amount × rate, the base currency's minor units),
   half away from zero), sum to zero. A base-currency line's base amount is its amount.
8. When every line shares one currency, the amounts also sum to zero in that currency.
9. (PRD-001 rev 0.167) Every line on a director's account has a `party` that is one of `people` and an
   `origin` that is one of `lent`, `paid_personally`, `pay`, `fee`, `paid_out` and `other`; (rev 0.207) every line
   on share application monies has a `party` that is one of `people` or `subscribers`, its subscriber, and no
   `origin`; no other line has either (FR-EQ-01, Appendix B.7). A scenario with no `people` has none, so any `party`
   on a director's account is refused. A journal's line names no instrument in the format, so a line on the account
   of role `convertible-instruments` is refused (FR-EQ-18: each line there names its SAFE; the engine's LEDGER-49).
10. (PRD-001 rev 0.55) A `reverse_on`, when there is one, is after the journal's date (FR-GL-02, JOURNAL-11:
    "a reversal date on or before the journal's date is refused when it posts").
11. (PRD-001 rev 0.171, 0.172) A line's dimension values are each a value in use of a dimension in use (DIM-06), and
    a line on an account that `requires` a dimension in use names a value of it (DIM-05), unless the journal's lines on
    that account sum to zero in the base currency: such a journal moves an amount between values. Section
    "Dimensions".

A reversal is rejected when its document is unknown or was rejected, when the document was
already reversed (a document is reversed at most once), or when it is dated before the document's
entry (FR-GL-12); a refused reversal leaves the document unreversed, so a later one can still post. A posted reversal negates every line of
the original entry as stored (amount and base amount; never re-converted, ENG-14), on its own date. After an
edit, the document's entry is the edit's replacement: a reversal negates it and is dated on or after it. An edit is
rejected for the reasons in section "Edits of a posted journal".

## Result

```json
{
  "schema": "fyniq-scenario-result@1",
  "name": "short-name",
  "entries": [
    {"document": "JNL-0001", "kind": "post", "posting_date": "2026-01-05",
     "lines": [{"account": "1010", "currency": "USD", "amount": "100.00", "base_amount": "134.72"},
               {"account": "4010", "currency": "SGD", "amount": "-134.72", "base_amount": "-134.72"}]},
    {"document": "JNL-0001", "kind": "reversal", "posting_date": "2026-01-06",
     "lines": [{"account": "1010", "currency": "USD", "amount": "-100.00", "base_amount": "-134.72"},
               {"account": "4010", "currency": "SGD", "amount": "134.72", "base_amount": "134.72"}]}
  ],
  "rejected": [],
  "trial_balance": {"2026-01": {}}
}
```

- `entries`: the posted documents in scenario order (kind `post`), then the posted reversals and edits in
  scenario order (a void's `reversal`; an edit's `reversal`, then its `replacement`), then the scheduled entries
  (section "P1 entries": releases, depreciation, automatic reversals, and the month-end `revaluation` and its
  `revaluation_reversal`); lines in the document's order.
- Every amount is a plain decimal string at exactly its currency's minor units (`"100.00"`,
  `"12345"`, `"2.263"`); a base amount at the base currency's. Zero is `"0.00"`, never `"-0.00"`.
- `rejected`: the refused documents (`"JNL-0003"`), reversals (`"JNL-0001/reversal"`) and edits
  (`"JNL-0001/edit"`) in the order they were refused, documents first. A key can repeat (a third reversal of one
  document, a second refused edit).
  No reason codes, so neither side depends on the other's codes; `python -m oracle run --explain`
  prints the oracle's reasons on stderr for people.
- `trial_balance`: each posting month (`YYYY-MM`) maps each account to its base-currency total for
  that month. The oracle leaves out accounts whose total is zero and keeps a month in which every
  account nets to zero (`{}`). **A comparator reads a missing account, or a missing month, as zero.**
- (PRD-001 rev 0.167) A posted line on a director's account carries its `party`, and a reversal's line
  keeps it; the comparator compares it with the line. `people`, present only when a posted line names
  a person, maps each person to each account to the base-currency total of every posted line naming
  them there: the sub-ledgers of 2500 and 2120 (FR-EQ-01) and of share application monies (rev 0.207). Totals of zero are left out, and
  a comparator reads a missing person or account as zero.
- A posted line on the instruments account carries its `instrument`, the ID of the `instrument` document it names;
  the comparator compares it with the line (FR-EQ-18).

## Decided in PRD-001 rev 0.24

These shapes were open until PRD-001 rev 0.24 decided them (ENG-24, the manual journal's validity
rules, and the rows each cites below); both sides follow the decision. The generator produces each
of them (task 3.4 of `p0-manual-journal-rules`), and [`golden/decided-rules.json`](golden/README.md)
posts one of each, its figures worked out by hand in `oracle/tests/test_golden.py`.

| Shape | Both sides | Requirement | Generated |
|---|---|---|---|
| A rate (or rate source) on a base-currency line | Rejected | ENG-03 (a rate entered on a base-currency line is refused); ENG-24 rule 5 | Generated (rate 1 or any rate, or the source alone) |
| A non-zero foreign line whose base amount rounds to zero (JPY 1 × 0.001) | Posts; the base amount is `0.00` | ENG-24 rule 3; INV-05 | Generated (half of them just under half a minor unit) |
| A computed base amount of 17 or more integer digits | Rejected (it does not fit NUMERIC(20,4)), never truncated | ENG-44; ENG-24 rule 6 | Generated (a quarter exactly 10^16) |
| A reversal dated before its document | Rejected; the document is not reversed, so a later reversal still posts | FR-GL-12 ("dated on or after the entry's posting date") | Generated (up to 20 days before) |
| A line to an account with the role `share-capital` | Rejected | INV-13; ENG-24 ("the invariants of §10.7 apply as well") | Generated |
| An amount written with trailing zeros beyond the minor units (`"12345.0"` for JPY) | Rejected | ENG-44 (decimal places counted as written) | Generated |
| A single-currency foreign journal that balances in its currency but not in base after `RND-FX` | Rejected (a manual journal has no rounding line) | ENG-24 rule 7 ("the engine never adds, drops or changes a line") | Generated |
| `version` | Read; a document posts the version it is written with, and (rev 0.54) an edit names the next one (section "Edits of a posted journal") | ENG-03 (an entry names its document's version), FR-GL-12 | Always 1; an edit's next, or (refused) another |
| `fx_rate_source` values | Any non-empty string; not used in the result | ENG-03, ENG-40 (a rate typed on the document has the source "document") | `"document"`; `"base"` too on a base-currency line built to be rejected |

## P1 sources (`cmd/scenario-run` reads every one)

From P1 a scenario may hold documents of other sources beside manual journals, each posted by its
rule set (PRD-001 §10.2, Appendix B; the oracle's `oracle/src/oracle/sources/`). The format grows
additively, still `fyniq-scenario@1`: a scenario of manual journals only is unchanged, and so is its
result. **The engine's `cmd/scenario-run` reads every P1 source**, each in `internal/differential/` (`p1.go` with
the bank, `p1_purchases.go`, `p1_sales.go`, `p1_claims.go`, `p1_assets.go`, `p1_founders.go` and `p1_merge.go`, the
account merges of rev 0.195; the engine-session
tasks of `p1-bank-import-reconcile`, `p1-purchases`, `p1-sales`, `p1-expense-claims` and `p1-fixed-assets`), so the
Go gate posts their golden cases in [`golden/`](golden/README.md) through both sides, and `make differential` draws
the sources the generator has, and the statements and KPIs over them
(`DIFFERENTIAL_SOURCES=bank,purchases,sales,claims,assets,founders,general-ledger,fx,statements,kpis` in the Makefile; the founders'
source gives the scenario two people, their director's accounts and a books' start, and both sides' `party` on each
line and `people` totals are compared). The engine states the statements and the KPIs too (sections "Statements" and
"KPIs"), and the general ledger's shapes of `p1-general-ledger-close` (task 8.1: the opening balances, automatic
reversals and edits, section "Opening balances, automatic reversals and edits"; golden case
[`golden/general-ledger.json`](golden/README.md), generator source `general-ledger`).

### Settings and `as_of`

A scenario with a P1 document must have `settings` and `as_of`, or it is invalid:

- `as_of` is the organisation's business date ("today", ENG-05). It is on or after every document's,
  reversal's and edit's date. Scheduled entries (a spread's releases, an asset's depreciation, a journal's
  automatic reversal) post when dated on or before it; later ones are the future and never post. A scenario
  with a `reverse_on` needs `as_of` too, even with manual journals only.
- `settings.accounts` names the system accounts by key (Appendix B.5): `receivable` (1100), `payable`
  (2010), `gst` (2200), `prepayments` (1210), `deferred_revenue` (2400), `fx` (7030), `bank_charges`
  (6460, a setting), `withholding_tax` (2330), `customer_deposits` (2410), `claims_payable` (2150),
  `advances` (1250), `director_loan` (2500), `deferred_remuneration` (2120), `depreciation` (6610), `accumulated_depreciation` (1590), `disposal` (7050, a setting),
  `disposal_clearing` (1160, a setting) and `unrealised_fx` (7040, B.5 rev 0.31; section "Month-end revaluation"). Each named account must be in `accounts`. A document that
  needs a key the organisation has not set is rejected.
- `settings.books_start` is the first day of the books: an asset's depreciation posts from the later of
  its start and this day's month (FR-FA-02), and the opening balances are dated on it (FR-GL-06).
- `settings.first_year_end` is the last day of the first financial year (FR-ORG-02; rev 0.195): with `books_start` it
  sets the financial years an income or expense account's merge reads (section "Account merges").
- `settings.mileage_rates` lists the rate per km for each vehicle type from each date (FR-AP-16; a
  policy setting under dual control, not a statutory rate): `{"vehicle": "car", "from": "2026-01-01",
  "rate": "0.60"}`.
- `settings.tolerance` holds the settlement tolerances of FR-BNK-08 (open question 32), each a plain
  decimal: `base` (S$1.00), `percent` (3), `per_document` (S$50.00) and `per_month` (S$200.00); a key
  left out takes that default.
- `settings.money_accounts` maps each bank, card, wallet or cash account to the currency it holds
  (FR-BNK-01); each must be in `accounts` and its currency in `currencies`.
- `settings.period_end_rates` gives each currency's rate for a month's last day, as a rate or as a quote stored under
  `RND-RATE`, which the month-end revaluation reads (section "Month-end revaluation").
- `settings.gst` holds the registration, `registered_from` and `registered_to` (both inclusive; no
  `registered_from` means never registered), `per_line` for `RND-GST-EXCL` per line instead of per
  subtotal, and `rates`, the standard rate as a percentage from each date (the sg-gst pack, ENG-31).
  A rate is in force from its date until the next one's.

A reversal names a manual journal: a reversal of a P1 document (a void) is not in the format yet, so it
makes the scenario invalid.

### The rules every P1 source shares

A P1 document is rejected whole (it posts nothing) when any of these fails, besides its own rules:

1. Amounts and rates meet the input boundary, as rule 1 of a manual journal (ENG-44); an amount is
   non-zero unless its source says otherwise.
2. An amount not in the base currency has `fx_rate` and its source (the rate cascade's result,
   ENG-40); a base-currency amount has neither.
3. Every account a line posts to exists, is active, is not a group account (INV-04) and is not a
   share-capital account (INV-13: only the register of share issues posts there, rev 0.49). A line on the account of
   role `convertible-instruments` names a recorded instrument and no other line names one (FR-EQ-18; the database's
   FY292): only a bank line's parts and a SAFE conversion name one in the format, so any other source's line there is
   rejected.
4. A GST code is one of Appendix B.6's twelve, of the direction its document takes: a bill's, a vendor
   credit's and a claim's receipt line take a purchase code (TX, IM, ZP, BL, NR, EP, OP; FR-AP-02's "GST
   input tax codes"); an invoice's and a credit note's lines, a part-payment deposit and a disposal's
   proceeds a supply code (SR, ZR, ES, OS; the output tax of FR-AR-09 and FR-FA-03); a bank line's part
   any of the twelve (Appendix B.1, a refund). NA, which has no direction, is only a bank line's part's.
   GST is charged when the organisation is registered on the document's date (its tax point) and the
   code is standard-rated (SR, TX, IM), at the rate in force then (a registered date with no rate in
   force rejects the document); otherwise it is 0.
5. A spread is 1 to 120 months (FR-GL-03); its deferral account takes the whole amount when the
   document posts, and each month's `ALLOC-EVEN` part is released to the category, dated the first day
   of its month or, for a month that starts before the document, the document's date. A month whose
   part is zero releases nothing.
6. Every base amount fits NUMERIC(20,4).

### P1 entries

A P1 document posts at most one entry, `kind` `post`, on its date (the opening balances too, on theirs, the books'
start; an edit adds a `reversal` and a `replacement`). To keep the comparison independent of
how each side splits a leg into lines, a P1 entry is written in its **canonical form**: one line per
(account, currency, person, instrument, dimension set), whose amount and base amount are the sums of that account's
lines in that currency naming that person, that instrument and exactly those dimension values (a director's account's
lines stay apart by person, FR-EQ-01, the instruments account's by instrument, FR-EQ-18, and every account's by
dimension set, FR-DIM-02 rev 0.171), lines whose sums are both zero left out, sorted by account, then currency, then
person (a line naming none first), then instrument (none first), then dimension set as section "Dimensions" writes it
(`""`, the lines with none, first). A manual journal's entries keep their lines in the journal's order, as before.

Scheduled entries come after every document, reversal and edit, `kind` `release` (a spread's),
`depreciation` (an asset's) or `auto_reversal` (a journal's): one entry per document, kind and date, ordered by
date, then by the document's place in the scenario, then by kind. A month's `revaluation` and its
`revaluation_reversal` (section "Month-end revaluation") are scheduled too, in canonical form, and come after every
document's scheduled entries of their date. A release and a depreciation are in canonical
form; an automatic reversal negates its journal's entry line by line, in its order, as a reversal does. A registered
asset posts no entry of its own.

### Bank lines (`bank_transaction`, `bank_transaction@1`) and transfers (`bank_transfer`, `bank_transfer@1`)

From Appendix B.1, §10.2 and PRD-001 rev 0.140 and 0.141.

```json
{"type": "bank_transaction", "id": "BT-0001", "version": 1, "date": "2026-03-05",
 "account": "1021", "currency": "USD", "amount": "-100.01", "fx_rate": "1.3335", "fx_rate_source": "document",
 "parts": [{"account": "6010", "amount": "-50.00", "gst_code": "OP"},
           {"account": "6110", "amount": "-50.01", "gst_code": "OP", "spread_months": 12}]}
```

A line posts: the money account its signed gross (money in positive) at `RND-FX`; per part, 2200 −GST,
GST = `RND-GST-INCL` of the part's `RND-FX` base amount, and the part's account (or, spread, 1210 for
money out and 2400 for money in) the rest, −(base − GST), as a base-currency line; 7030 the cent `RND-FX`
leaves between the parts and the line. Spreads release from the posting month. It is rejected when the
account is not a money account in the line's currency, it has no parts, a part is zero or of the other
direction, the parts do not add up to the line, a part is on a money account (a transfer, FR-BNK-09)
or on the receivable or payable account (a match, FR-BNK-07), or a spread is not on an expense
(money out) or a revenue (money in) account.

```json
{"type": "bank_transfer", "id": "TR-0002", "version": 1, "date": "2026-04-03",
 "from": {"account": "1010", "currency": "SGD", "amount": "1000.00"},
 "to": {"account": "1021", "currency": "USD", "amount": "738.00", "fx_rate": "1.3485", "fx_rate_source": "document"},
 "fee": "4.50"}
```

A transfer posts once: the receiving account what it received and the paying account what it paid,
each leg at its own currency's rate (`RND-FX`); a `fee`, in the paying currency and part of what was
paid, to `bank_charges`; whatever is left to 7030. It is rejected when a leg is not a money account in
its currency, both legs are one account, an amount is not positive, the fee is not below what was paid,
or, in one currency, what was paid is not what was received plus the fee.

### Bills (`bill`, `bill@1`), vendor credits (`vendor_credit`, `vendor_credit@1`) and payments of bills (`bill_payment`, `bill_payment@1`)

From §10.2, Appendix B.2, ENG-42, FR-AP-02, FR-AP-03, FR-AP-19, FR-AP-20, FR-GL-03, FR-BNK-08 and the rows
of rev 0.74 to 0.93.

```json
{"type": "bill", "id": "BILL-0001", "version": 1, "date": "2026-03-05", "currency": "USD",
 "fx_rate": "1.34", "fx_rate_source": "document",
 "lines": [{"account": "6010", "amount": "1000.00", "gst_code": "TX", "gst": "90.00", "gst_base": "121.50"},
           {"account": "6110", "quantity": "3", "unit_price": "12.345678", "gst_code": "NR",
            "spread_months": 12, "spread_from": "2026-01-01"}]}
```

A line's net is its `amount`, or `RND-LINE` of `quantity` × `unit_price` (with `quantity_before`, what
earlier bills took of a purchase order's line, the part's `RND-LINE` difference). `gst` is the GST the
tax invoice states in the bill's currency (0 when absent); `gst_base`, on a bill in another currency,
the GST it states in the base currency (ENG-42); `business_share` the percent claimable (100 when
absent). A line's GST is claimable when its code is TX or IM and the organisation is registered on the
bill's date; then 2200 takes `RND-GST-SHARE` of the stated GST (of `gst_base` for a bill in another
currency, and nothing when it states none) and the rest of the GST stays in the line's cost. The cost,
net plus the GST not claimed, goes to the line's account at `RND-FX`, or, spread (an expense line, 1 to
120 months, from the month of `spread_from`, by default the bill's), to 1210 in the base currency,
released monthly. 2010 takes the total (nets and stated GST) at `RND-FX`, less, with `withholding`
(`{"percent": "15"}`), the supplier's amount withheld, while 2330 takes `RND-WHT` of the net total in the
base currency. 7030 takes what balances the entry. A vendor credit posts a bill's legs negated and is
never spread or withheld. A bill is rejected when it has no lines, a line has both or neither of an
amount and a quantity, a line's GST code is not a purchase code (rule 4: NA and the supply codes are
refused), its total is not positive, a spread is not on an expense with a cost, or `gst_base` is on a
base-currency bill; a vendor credit likewise.

```json
{"type": "bill_payment", "id": "PAY-0001", "version": 1, "date": "2026-03-20", "account": "1021",
 "currency": "USD", "fx_rate": "1.30", "fx_rate_source": "document",
 "parts": [{"document": "BILL-0001", "amount": "1090.00"}], "unapplied": "10.00"}
```

A payment settles posted bills in its currency, at most what each has open (what it owes after
withholding, less what earlier payments settled): 2010 takes each part at the bill's stored rate, and
the part that settles what is left of a bill clears all that is left of its 2010 line (the residual).
A part `in_full` settles all that is open though it pays less, when the difference is within the
tolerances (above). `unapplied` stays on 2010 as a credit with the supplier, at the payment's rate.
The money account takes the total at the payment's rate when it holds the bills' currency; a
base-currency account paying bills in another currency gives what it paid (`base_amount`) or a rate.
7030 takes what balances the entry (realised FX, the `RND-FX` cents, a difference in full). It is
rejected when a part names no posted bill, a bill twice or a bill in another currency, a part is above
what is open, a difference is beyond tolerance, or it settles nothing.

### Invoices (`invoice`, `invoice@1`), credit notes (`credit_note`, `credit_note@1`), receipts (`customer_receipt`, `customer_receipt@1`), deposits (`customer_deposit`, `customer_deposit@1`; `deposit_refund`, `deposit_refund@1`) and write-offs (`write_off`, `write_off@1`)

From §10.2, Appendix B.2, ENG-42, FR-AR-03, FR-AR-04, FR-AR-09, FR-AR-17, FR-AR-18, FR-GL-03 and the rows of
rev 0.116 to 0.128.

```json
{"type": "customer_deposit", "id": "DP-0001", "version": 1, "date": "2026-03-01", "account": "1010",
 "currency": "SGD", "amount": "1090.00", "kind": "part_payment", "gst_code": "SR"}
```

A deposit puts its gross on the money account (in its currency) at `RND-FX`; a `part_payment` bears GST,
`RND-GST-INCL` of the gross in base when registered and the code carries it, to 2200; 2410 takes the
rest. A `security` deposit names no GST code. A `deposit_refund` (`deposit`, `account`, `amount` in the
deposit's currency, a rate when foreign) pays back at most what is left: 2410 its base part less its GST
part (`RND-DEPOSIT`), 2200 the GST part, the money account what left it, 7030 the realised FX.

```json
{"type": "invoice", "id": "INV-0001", "version": 1, "date": "2026-03-31", "currency": "SGD",
 "lines": [{"account": "4010", "amount": "2000.00", "gst_code": "SR"},
           {"account": "4020", "quantity": "3", "unit_price": "10.00", "discount": "12.5", "gst_code": "ZR",
            "spread_months": 12}],
 "deposits": [{"deposit": "DP-0001", "amount": "1090.00"}]}
```

A line's net is its `amount`, or `RND-LINE` of `quantity` × `unit_price` less a `discount` (0 to 100,
two decimals). Each net goes to its account at `RND-FX`, or, spread (a revenue line), to 2400 in the base
currency, released monthly from the month of `spread_from` (the invoice's by default). When registered on
its date, 2200 takes `RND-GST-EXCL` of the base nets per GST code (per subtotal, or per line as
`settings.gst.per_line` says). 1100 takes the customer's total, the nets and `RND-GST-EXCL` of them in the
invoice's currency, at `RND-FX`, less what the deposits take; 7030 what balances. Each deposit it deducts
(posted, in its currency, at most what is left of it, together at most its total) takes, under
`RND-DEPOSIT`, its part of the deposit's base amount and of the GST the deposit bore: 2410 takes the base
part less the GST part, and the output GST line is reduced by the GST part. A credit note posts an
invoice's legs negated; it is never spread and deducts no deposit; with `invoice` it names the invoice
it credits (in its currency, dated on or before it, at most its total) and takes GST at that invoice's
date. An invoice or a credit note whose line's GST code is not a supply code (rule 4: NA and the
purchase codes are refused) is rejected, as is a part-payment deposit whose code is not one.

A `customer_receipt` is a settlement (the shape of a payment of bills) of posted invoices on 1100,
mirrored: 1100 is credited, the money account debited, a part in full is a loss on 7030, and `unapplied`
stays a credit with the customer. A `write_off` (`invoice`, `amount` at most what is open, `account` the
reason's) credits 1100 at the invoice's stored rate (all that is left of its line when it takes all that
is open) and debits the account with that carrying amount, in the base currency.

### Expense claims (`expense_claim`, `expense_claim@1` and `@2`), reimbursements (`claim_reimbursement`, `claim_reimbursement@1`) and cash advances (`cash_advance`, `cash_advance@1`)

From §10.2, §10.4, Appendix B.7, FR-AP-07, FR-AP-16, FR-AP-17, FR-AP-20, FR-EQ-02, UC-EXP-03, the rows of
rev 0.147, 0.148, 0.151, 0.167 and 0.169, and the owner's decision on foreign claims (SFRS(I) 1-21)
[accountant to confirm].

```json
{"type": "expense_claim", "id": "EC-0001", "version": 1, "date": "2026-03-05", "currency": "SGD",
 "lines": [{"account": "6010", "amount": "100.00", "gst_code": "TX", "gst": "9.00"},
           {"account": "6110", "distance": "12.35", "vehicle": "car"}]}
```

A receipt line is costed as a bill's line (a purchase code, rule 4; its input tax claimable on TX and IM
when registered, its `business_share`). A trip (`distance` in km and `vehicle`, never a GST code) costs
`RND-MILEAGE` of the distance at the vehicle's rate in force on the claim's date, with no GST, and only
in a base-currency claim. Each line's cost goes to its account in the claim's currency at `RND-FX`, the
input tax to 2200. What the employee is owed is in the **base currency** (FR-AP-17; SFRS(I) 1-21: the
company owes a fixed SGD amount, what they were out of pocket): for each receipt line, what the claimant
states they paid for it (`paid`, below) or else its net plus its stated GST at the claim's rate under
`RND-FX`, line by line, summed, and each trip's amount. It goes first to 1250, up to what is left of the
`advance` it settles (a claim in any currency: what it owes is in the base currency, rev 0.169, and
UC-EXP-03's IDR receipts settle an SGD advance), and the rest to 2150, a base-currency line; 7030 takes
what balances (the `RND-FX` cents between a line's parts, the stated GST in base against the GST at the
rate, and what was paid against the line at the rate).

**What the claimant paid** (`paid`; FR-AP-17 rev 0.169: "2150 takes each receipt line's amount, net and
GST, at the claim's rate under `RND-FX`, line by line, or the SGD the claimant states they paid, with the
difference to 7030 when the claim posts") [accountant to confirm]. A receipt line of a claim in another
currency may state, in the base currency, what left the claimant's account for that receipt, net and GST
together (a card's or a wallet's charge, which carries its own rate and spread). Here S$150.00 is advanced,
and a claim in USD settles it:

```json
{"type": "cash_advance", "id": "ADV-0003", "version": 1, "date": "2026-04-12", "account": "1011", "amount": "150.00"}
```

```json
{"type": "expense_claim", "id": "EC-0009", "version": 1, "date": "2026-04-14", "currency": "USD",
 "fx_rate": "1.35", "fx_rate_source": "document", "advance": "ADV-0003",
 "lines": [{"account": "6010", "amount": "100.00", "gst_code": "TX", "gst": "9.00", "gst_base": "12.20",
            "paid": "148.02"},
           {"account": "6110", "amount": "20.00", "gst_code": "OP"}]}
```

- **What is owed.** For a line with `paid`, 2150 (or 2500 for a loan) takes `paid` instead of the line's
  net and GST at the claim's rate; a line without it is owed at the rate as before, and the claim owes
  the sum. The reimbursement pays that sum in the base currency, with no FX.
- **The difference.** `paid` changes only what is owed: the line's cost still goes to its account in the
  claim's currency at the claim's rate, and its input tax to 2200 as without it. 7030 takes what balances,
  so `paid` above the line at the rate is a debit to 7030 and below it a credit, in the claim's entry.
- **With `gst_base`.** The two are independent: `gst_base` (the GST stated in the base currency, ENG-42)
  sets the input tax 2200 claims, `paid` what is owed; neither is checked against the other, and 7030
  takes both differences. Above, registered: 6010 USD 100.00 at 135.00 and 6110 USD 20.00 at 27.00; 2200
  12.20; owed 148.02 (not `RND-FX` of 109.00 × 1.35 = 147.15) + 27.00 = 175.02; 7030 the 0.82 that
  balances (0.87 paid above the rate less the 0.05 of GST stated above 9.00 × 1.35).
- **With an advance.** What is owed, `paid` included, goes first to 1250 up to what is left of the
  advance, the rest to 2150: with 150.00 left of ADV-0003, 1250 −150.00 and 2150 −25.02.
- **No tolerance.** Any `paid` above zero is owed as stated, however far from the line at the rate (P1
  readings).
- **Refused.** The claim is rejected whole when `paid` is on a line of a base-currency claim (that line's
  amount is already what was paid) or on a trip (a trip is costed at the mileage rate, in a base-currency
  claim only), or is not a valid base-currency amount (rule 1: a plain decimal at most at the base
  currency's minor units), or is not above zero.

A `claim_reimbursement` is a settlement (the shape of a payment of bills) of posted claims on 2150 in
the base currency only, from a base-currency money account, at most what each owes, with no rate and so
no FX: it is rejected whole when its `currency` is not the base, or it has `fx_rate`, `fx_rate_source`,
`base_amount`, `unapplied` or a part `in_full`. A `cash_advance` (a base-currency money account and an
amount) posts Dr 1250, Cr the money account; with `refund_of` it is what is left of that advance coming
back, at most what is left: Dr the money account, Cr 1250.

A claim kept as a loan to the company (`"loan": "<person>"`, one of `people`; FR-EQ-02, `expense_claim@2`)
credits `director_loan` (2500) instead of 2150, as 2150 would be credited (in the base currency), and the
line names the person (`party`): what the director paid stays owed to them there, `paid` included. It
settles no `advance`, and no reimbursement settles it.

### Costs before the books start (`pre_incorporation_cost`, `pre_incorporation_cost@1`)

From FR-EQ-03, Appendix B.7 and the founders' money design D7 (rev 0.167).

```json
{"type": "pre_incorporation_cost", "id": "PIC-0001", "version": 1, "date": "2026-01-01", "incurred": "2025-11-03",
 "person": "P0001", "account": "1500", "gross": "3815.00", "gst": "315.00", "claim_gst": true}
```

The cost as the accountant decided it: `date` is the day it posts (on or after `settings.books_start`), `incurred` the
day the person paid it (before the books start), `person` one of `people`. It posts Dr `account` with `gross`, or with
`gross` less `gst` when `claim_gst` (only while GST-registered on `date`), Dr `gst` (2200) with the GST claimed, and Cr
`director_loan` (2500) with `gross`, the line naming the person. The account is not a director's account, a money
account or GST control.

### Directors' fees (`director_fee`, `director_fee@1`)

From FR-EQ-04, §10.2 and the founders' money design D8 (rev 0.167).

```json
{"type": "director_fee", "id": "DF-0001", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "6050",
 "amount": "6000.00"}
```

A fee the shareholders voted, posted on `date`, the day the director became entitled to it: Dr `account` (6050
Directors' fees) with `amount`, Cr `deferred_remuneration` (2120) with `amount`, the line naming `person`, one of
`people`. The account is not a director's account, a money account or GST control.

### Waivers of founders' balances (`director_waiver`, `director_settlement@1`)

From FR-EQ-07, Appendix B.7 and the founders' money design D10 (rev 0.167).

```json
{"type": "director_waiver", "id": "DS-0001", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
 "credit": "3300", "amount": "400.00"}
```

A waiver of what the company owes a director or shareholder, posted on `date`, the day it was approved: Dr `account`
(2500 or 2120, a director's account) with `amount`, at most what is owed there ("P1 readings"), the line naming `person`, one of `people`; Cr `credit` (3300 Capital
contribution, or an income account) with `amount`. The credit is not a director's account, a money account or GST
control. A loan kept posts nothing.

### Repayments to founders (`director_repayment`, `director_repayment@1`)

From FR-EQ-05, FR-BNK-12, Appendix B.7 and the founders' money design D10 (rev 0.167).

```json
{"type": "director_repayment", "id": "DR-0001", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "1010",
 "settlements": [{"account": "2500", "amount": "300.00"}]}
```

What a payment batch's release repaid a director or shareholder, posted on `date`, the batch's payment date: for each
of `settlements` Dr its `account` (2500 or 2120, a director's account) with its `amount`, the line naming `person`, one
of `people`; Cr `account`, a money account in the base currency, with the total. The settlements on one account total
at most what is owed there ("P1 readings").

### Conversions of founders' balances to shares (`director_conversion`, `share_issue@1`)

From FR-EQ-06, FR-EQ-08, Appendix B.7, `RND-SHARE` and the founders' money design D11 (rev 0.167).

```json
{"type": "director_conversion", "id": "SI-0001", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
 "class": "3010", "amount": "100.00", "price": "1.20"}
```

A founder's balance converted to shares, posted on `date`, the day ACRA's register shows the allotment; `amount` is at
most what is owed on `account` ("P1 readings"). `RND-SHARE`
allots n whole shares for `amount` at `price` (83 shares here, 99.60 converted, 0.40 left owed): Dr `account` (2500 or
2120, a director's account) with round(n × `price`), the line naming `person`; Cr `class` (a share-capital account) with
the same, or, when `fair_value_price` is given, with round(n × `fair_value_price`) and the difference to `difference`, an
income or expense account (Dr a loss, Cr a gain).

### Cash share issues (`share_issue_cash`, `share_issue_cash@1`) and their money from the bank

From FR-EQ-17, Appendix B.7 and PRD-001 rev 0.205 to 0.207 (G4-043).

```json
{"type": "bank_transaction", "id": "BT-0101", "version": 1, "date": "2026-03-02", "account": "1010", "currency": "SGD",
 "amount": "600000.00", "person": "P0001", "parts": [{"account": "2040", "amount": "600000.00", "gst_code": "NA"}]}
```

```json
{"type": "share_issue_cash", "id": "EQ-0101", "version": 1, "date": "2026-03-12", "person": "P0001", "class": "3010",
 "shares": "250000", "price": "2.40"}
```

Money for shares arrives, or is refunded, on a bank line (`bank_transaction@1`, the owner's decision of 7 Nov 2026):
its parts on share application monies post as any part does, each naming the line's `person`, the subscriber, one of
`people` or `subscribers`. A line with such a part and no `person`, a `person` who is neither, or a `person` on a line
with no such part, is rejected.

A cash issue to one subscriber posts on `date`, the day ACRA's register of members shows the allotment. Its amount is
round(`shares` × `price`) under `RND-LINE`: Dr share application monies (the one active account of role
`share-application-monies`) with the amount, the line naming `person`; Cr `class` (a share-capital account) with the
same. It is never made effective unpaid: it is rejected when what the subscriber's monies hold at the end of `date`
(the credit of the lines naming them there in the earlier documents, as "P1 readings" reads what is owed on a
director's account) is less than the amount. It is rejected too when `shares` is not a whole number above zero,
`price` not above zero with at most 6 decimals, `class` not share capital, `person` neither of `people` nor
`subscribers`, or the chart has no share application monies.

### Share transfers (`share_transfer`)

From FR-EQ-17 and the equity register's design D3 and D4 (`p1-equity-register`).

```json
{"type": "share_transfer", "id": "EQ-0102", "version": 1, "date": "2026-04-01", "seller": "P0001", "buyer": "P0002",
 "class": "3010", "shares": "50000"}
```

A transfer moves `shares` of `class` from `seller` to `buyer` on `date`, the day ACRA's register of members shows it,
and posts no entry: the result has no entry for it, and a transfer that is not rejected is not in `rejected` either.
It is rejected when `shares` is not a whole number above zero, `class` is not an active share-capital account that is
not a group, `seller` or `buyer` is neither of `people` nor `subscribers`, the two are the same, or the seller's
holding of `class` would fall below zero at the end of any day from `date` on (the database's FY288: "no holding
below zero on any day"), reading the holdings the earlier documents moved ("Cap tables").

### SAFEs and their conversion (`instrument`; `safe_conversion`, `instrument_conversion@1`)

From FR-EQ-18, Appendix B.7 ("SAFE or note received; converted"), `RND-SHARE`, Appendix G.3 (G4-044) and the equity
register's design D6 (`p1-equity-register`).

```json
{"type": "instrument", "id": "SAFE-0001", "version": 1, "date": "2026-02-01", "person": "P0002",
 "form": "post_money_safe", "amount": "125000.00", "valuation_cap": "2500000", "class": "3010", "price_decimals": 4}
```

An instrument records a SAFE on `date`, the day it was signed, and posts no entry: the result has no entry for it, and
one that is not rejected is not in `rejected` either. Its holder `person` is one of `people` or `subscribers`. `form`
is one of:

- `post_money_safe` or `pre_money_safe`, each with a `valuation_cap`;
- `discount_safe`, with a `discount`;
- `mfn_safe`, a most-favoured-nation clause, with neither.

A capped form may have a `discount` too. A `valuation_cap` on another form is kept and does not price it, as the
product keeps it. `amount` is in the base currency, above zero; `class` is the share-capital account it converts into
(active, not a group); `price_decimals`, 0 to 6, is the conversion price's precision. `valuation_cap` is a plain
decimal above zero with at most 12 integer digits and 8 decimals, and `discount` a plain decimal above 0 and below 100
with at most 8 decimals. It is rejected when any of these fails. A convertible note's interest and maturity are not in
the format yet.

Its money comes in on a bank line (`bank_transaction@1`), as a subscriber's does: each part on the instruments account
(the account of role `convertible-instruments`) names the line's `instrument`, an instrument an earlier document
recorded. A line with such a part and no `instrument`, an `instrument` no earlier document recorded, or an `instrument`
on a line with no such part, is rejected. One line may carry a subscriber's money and a SAFE's, each part naming its
own:

```json
{"type": "bank_transaction", "id": "BT-0103", "version": 1, "date": "2026-04-10", "account": "1010", "currency": "SGD",
 "amount": "155000.00", "person": "P0002", "instrument": "SAFE-0001",
 "parts": [{"account": "2040", "amount": "30000.00", "gst_code": "NA"},
           {"account": "2700", "amount": "125000.00", "gst_code": "NA"}]}
```

```json
{"type": "share_issue_cash", "id": "EQ-0104", "version": 1, "date": "2026-04-15", "person": "P0002", "class": "3010",
 "shares": "10000", "price": "3.00"}
```

```json
{"type": "safe_conversion", "id": "EQ-0105", "version": 1, "date": "2026-04-15", "round": "EQ-0104"}
```

A conversion converts on `date`, the day ACRA's register of members shows the allotments, every instrument recorded
before it that no earlier conversion converted, at the priced round `round`: an earlier cash issue
(`share_issue_cash`) that was not rejected, dated on or before `date`. With the round's `price` P, and C₀ the shares
issued at the end of the day before the round's date (the cap table's `issued` then; section "Cap tables"), each
instrument's conversion price is worked exactly:

- f is the sum of `amount` ÷ `valuation_cap` over the post-money SAFEs converting: the part of the company they convert
  into;
- a post-money SAFE's cap price is `valuation_cap` × (1 − f) ÷ C₀: its cap divided by the company's capitalisation once
  the SAFEs have converted, C₀ ÷ (1 − f), which counts their own shares. With one SAFE, or caps alike, this is
  (cap − Σ amount) ÷ C₀;
- a pre-money SAFE's cap price is `valuation_cap` ÷ C₀;
- the discounted round price is P × (100 − `discount`) ÷ 100, or P with no discount;
- the conversion price is the lower of the cap price (for a capped form) and the discounted round price, cut down
  (toward zero) to `price_decimals` decimals.

`RND-SHARE` then allots n whole shares for `amount` at that price, and round(n × price) converts; the rest stays on the
instruments account until it is settled under the terms. The conversion is rejected when no instrument converts, f is
1 or more, C₀ is zero, a price is zero once cut, an n is zero, the chart has no active instruments account or more
than one, or what the instruments account holds for an instrument at the end of `date` (the credit of the lines naming
it in the earlier documents) is less than what it converts.

It posts one entry on `date`, in canonical form: for each instrument, Dr the instruments account with what it
converts, the line naming the instrument, and Cr its `class` with the same. Each instrument's n shares move into its
holder's holding of its class on `date` (section "Cap tables").

G4-044 ([`golden/safe-conversion.json`](golden/README.md)) pins it:

- 1,000,000 founder shares, the post-money SAFE of S$125,000.00 with a S$2,500,000 cap, and the round of 200,000
  shares at S$3.00. f = 0.05 and the cap price is 2,500,000 × 0.95 ÷ 1,000,000 = 2.375: 52,631 shares, S$124,998.63
  converted (52,631 × 2.375 = 124,998.625), S$1.37 left. The cap table reads 79.83%, 15.97% and 4.20%.
- G.3's pre-money SAFE with a 20% discount: 2.50 against 3.00 × 0.80 = 2.40, so 52,083 shares, S$124,999.20, S$0.80
  left.
- Two post-money SAFEs, S$125,000.00 at a S$2,500,000 cap and S$250,000.00 at S$5,000,000, at a round of S$5.00: f =
  0.1, cap prices 2.25 and 4.50, 55,555 shares each (S$124,998.75 and S$249,997.50).

In the examples above, the round of 10,000 shares at S$3.00 follows the earlier examples' 250,083 shares (C₀), so the
SAFE's cap price is 2,500,000 × 0.95 ÷ 250,083 = 9.4968…; the round's S$3.00 is lower, so it converts 41,666 shares
for S$124,998.00, and S$2.00 stays on 2700.

Readings the PRD leaves open, each [accountant to confirm]:

| Shape | Oracle | Why |
|---|---|---|
| The shares the terms count before the round (C₀) | The shares issued at the end of the day before the round's date | D6's C₀ counts "shares, options and the pool, as the template says"; the format has no option pool yet [verify the terms with question 33's lawyer] |
| A capped SAFE whose cap price is above the round's | It converts at the round's price, the lower | FR-EQ-18: "the lower of the cap price and the discounted round price"; the product asks the accountant to check a post-money SAFE's capitalisation then |
| The conversion price's precision | Cut down, never rounded up | So the shares never cost more than the terms allow (D6) |
| One entry for the round's conversions | One entry per `safe_conversion`, a line pair per instrument | The product posts one entry per instrument (`instrument_conversion@1`); in canonical form their sums are the same |

### Fixed assets (`fixed_asset`, `asset_depreciation@1`) and disposals (`asset_disposal`, `asset_disposal@1`)

From Appendix B.4, §10.2, FR-FA-01, FR-FA-02, FR-FA-03 and the rows of rev 0.149, 0.150 and 0.151.

```json
{"type": "fixed_asset", "id": "FA-0001", "version": 1, "date": "2026-01-15", "account": "1500",
 "cost": "3600.00", "residual": "0.00", "life_months": 36, "depreciate_from": "2026-02-01"}
```

An asset in the register (an asset account, its cost and residual in the base currency, its life in
months, and its start, the month of `depreciate_from` or of its date) posts nothing itself. It schedules
straight-line depreciation: month k of its life takes the k-th part of `ALLOC-EVEN`(cost − residual,
life), Dr `depreciation` · Cr `accumulated_depreciation`, dated the month's last day, from the later of its
start and the books' start, once that day is on or before `as_of`. It is rejected on an account that is
not an asset, a residual that is not below the cost, or a life of 0 or above 1,200 months.

```json
{"type": "asset_disposal", "id": "AD-0001", "version": 1, "date": "2026-04-10", "asset": "FA-0001",
 "proceeds": {"account": "1010", "amount": "3270.00", "gst_code": "SR"}}
```

A disposal credits the asset's account with its cost and debits `accumulated_depreciation` with its months
before the disposal's month (the opening balances' included); proceeds into a base-currency money account
bear GST, `RND-GST-INCL` of the amount, when registered and the code carries it; proceeds on
`disposal_clearing` (an invoice to the buyer credited it) bear none here; no `proceeds` is a write-off.
`disposal` takes the gain or loss, whatever balances. No month from the disposal's on depreciates. It is
rejected for an asset not registered or already disposed of, dated before the asset, proceeds received
whose GST code is not a supply code (rule 4, whatever the registration), or proceeds received with no
code while the organisation is registered on the disposal's date: a registered seller's sale is a
taxable supply, and 2200 takes its GST at "the proceeds' GST code's rate" (FR-FA-03). While not
registered a code is optional, and proceeds received with none bear no GST.

### Opening balances, automatic reversals and edits

The general ledger's shapes of `p1-general-ledger-close` (task 8.1), from FR-GL-01, FR-GL-02, FR-GL-06, FR-GL-12,
§10.2 ("Opening balances: as entered at books start; must net to zero"; "Edits of posted documents: reversal of
the previous version's entries + entries of the new version"), INV-13, ENG-14 and ENG-24, as PRD-001 rev 0.48 to
0.66 built and rev 0.58 confirmed them. A scenario of manual journals with none of them is unchanged, and so is its
result. `cmd/scenario-run` reads them (`internal/differential/differential.go`; `golden/general-ledger.json`). What the PRD leaves open is in
"P1 readings" (its last rows), read by the oracle session of task 8.1 and not yet recorded in the PRD: each stands
until the accountant reviews it.

#### Opening balances (`opening_balances`, `opening_balances@1`)

```json
{"type": "opening_balances", "id": "OB", "version": 1, "date": "2026-01-01", "memo": "Trial balance at 31 Dec 2025",
 "lines": [{"account": "1010", "currency": "SGD", "amount": "25000.00"},
           {"account": "1500", "currency": "SGD", "amount": "3600.00"},
           {"account": "3010", "currency": "SGD", "amount": "-10000.00"},
           {"account": "2500", "currency": "SGD", "amount": "-18600.00", "party": "P0001", "origin": "lent"}]}
```

Each account's balance on the day the books start, as journal lines (debit positive). The organisation has one
set ("one document, saved as a draft a version at a time and posted once"), so a scenario has at most one
`opening_balances`; a second makes it invalid. They are dated `settings.books_start` ("posted once, as entered, at
the books start"): a scenario with opening balances needs `settings` with `books_start`, and `as_of`, and any other
`date` makes it invalid, since the product never dates them otherwise.

They post one entry, kind `post`, on the books' start, when they meet a manual journal's rules 1 to 9 (ENG-24; rev
0.66: "the content must pass the posting rules"), with two differences:

- **Every line is in the base currency** ("every line is in the base currency, a line in another currency being
  refused (LEDGER-17), because open invoices and bills in other currencies come with sales and purchases"). A line
  in another currency rejects them, with or without a rate; a rate on a base-currency line rejects them (rule 5).
- **Share capital may take a line** (rev 0.49: "share capital may take a line"; INV-13: "Share-capital accounts
  change only through the register of share issues or opening balances"). A manual journal, and a journal's edit,
  still may not (rule 4).

So they are rejected whole when they do not net to zero (the balance check, Appendix C.1 #6, LEDGER-09), have fewer
than two lines or a zero line, reach an unknown, inactive or group account, or put a line on a director's account
without its person and origin, a line on share application monies without its subscriber, or a person on another
account (rule 9: "every journal line on 2500 or 2120 carries that contact as its sub-ledger key", FR-EQ-01 rev 0.167).
Their entry is in the canonical form of a P1 entry (one
line per account, currency, person and dimension set; the opening balances' guide: "each account's balance"), which must keep two
lines and fit NUMERIC(20,4).
A director's balance credited here is what the company owes the person from the books' start, so a waiver,
repayment or conversion after it may draw on it ("P1 readings": what is owed reads the entries of the documents
before it). They are never voided (a reversal naming them makes the scenario invalid): they change by an edit
(rev 0.66), below.

#### Automatic reversals (`reverse_on`, FR-GL-02)

```json
{"type": "manual_journal", "id": "JNL-0002", "version": 1, "date": "2026-03-31", "memo": "March accrual",
 "reverse_on": "2026-04-01",
 "lines": [{"account": "6010", "currency": "SGD", "amount": "1200.00"},
           {"account": "2999", "currency": "SGD", "amount": "-1200.00"}]}
```

A manual journal may carry `reverse_on`, "the date it reverses by itself, after its own date (JOURNAL-11)": one on
or before the journal's date rejects the journal whole (rule 10). "Once the journal posts, the scheduler reverses
it on that date", with no approval of its own, once the day is on or before `as_of` ("each posted journal due by
its organisation's own today", rev 0.55; ENG-05); a later one waits and does not post.

The automatic reversal is a scheduled entry, kind `auto_reversal`, dated `reverse_on`, placed with the releases and
depreciation (section "P1 entries"). It negates the journal's entry as stored, line by line in its order, amount
and base amount (ENG-14), as a reversal does. The scheduler runs once, at `as_of`, after every document, reversal
and edit of the scenario, so:

- a journal reversed by then (a void in `reversals`, whatever its date) has nothing left to reverse ("an entry is
  reversed at most once"): no automatic reversal posts, and nothing is refused;
- a journal edited by then reverses its current entry, the edit's replacement, on its current version's
  `reverse_on` ("the reversal is part of the journal as it was approved"); an edit without `reverse_on` takes the
  reversal away.

A scenario cannot edit a journal after its automatic reversal (the product refuses that, as any change to a
reversed journal).

#### Edits of a posted journal (an `edit` in `reversals`)

```json
{"document": "JNL-0002", "edit": {"version": 2, "date": "2026-03-31", "memo": "March accrual, corrected",
 "reverse_on": "2026-04-01",
 "lines": [{"account": "6010", "currency": "SGD", "amount": "1350.00"},
           {"account": "2999", "currency": "SGD", "amount": "-1350.00"}]}}
```

An edit is the next version of a posted manual journal or of the opening balances, written in `reversals` at the
place it is made, with the content a document of that type has (`version`, `date`, `memo`, `lines` and, for a
journal, `reverse_on`). "In one transaction it saves the next version, reverses the entry the journal had on that
entry's own date and posts the new version's entry on its date" (rev 0.54; rev 0.58: "the replaced entry is
reversed on its own date, so an edit makes its month right"). So it posts two entries at its place in the result,
both or neither ("the database refuses an edit without both postings"):

1. kind `reversal`: the journal's current entry negated as stored, line by line, on that entry's own date;
2. kind `replacement`: the new version's entry on its `date`, a journal's in its line order, the opening balances'
   in canonical form.

The replacement becomes the journal's entry: a later void negates it and is dated on or after it, a later edit
reverses it, and its `reverse_on` is the one the scheduler reads. The opening balances' edit posts both at the books'
start (rev 0.66: "a reversal of the entry they had and the new entry, both at the books start"): its `date` is the
books' start and it has no `reverse_on`, or the scenario is invalid. An edit naming a P1 document makes the scenario
invalid (edits of other documents are not in the format yet), as a reversal of one does.

An edit is rejected (`"<id>/edit"` in `rejected`) and changes nothing, so a later edit naming the same version can
still post, when:

1. its document never posted: unknown, or rejected (rev 0.54: "an edit is its own command on a posted journal");
2. its document is reversed by a void earlier in `reversals` ("the database refuses ... any change to a reversed
   journal");
3. its `version` is not the document's current version plus one ("it saves the next version"; FR-GL-01 rev 0.48:
   "at the version read"): a document's version is the one it is written with, then each posted edit's;
4. its content breaks its document's rules: a journal's rules 1 to 10, or the opening balances' rules above.

### Account merges (`account_merge`, `account_merge@1`)

From FR-COA-04 and Appendix B.8 (PRD-001 rev 0.174).

```json
{"type": "account_merge", "id": "AM-0001", "version": 1, "date": "2026-10-31", "account": "6120", "into": "6100"}
```

The merge of `account` into `into`, posted on `date`, the day the balance moves. It moves what `account` holds into
`into`, each currency and dimension set apart, at its recorded base value (ENG-14: never converted again):

- A balance-sheet account (asset, liability or equity) holds the sum of its lines dated on or before `date`; an income
  or expense account the sum of its lines dated from the first day of the financial year that holds `date` to `date`,
  since earlier years are in retained earnings. The financial years are those of `settings.books_start` and
  `settings.first_year_end`: the first runs from the one to the other, each later one from the day after the last ends
  to one year later less a day (the first may be short or long, FR-ORG-02). A merge of an income or expense account in
  a scenario without both is invalid.
- What it holds is read from the entries posted before the merge: those of the documents before it in the scenario
  (reversals and edits come after every document, so none counts), and their scheduled entries dated on or before
  `date` (rule 5 below keeps any later one from reaching `account`).
- For each currency and dimension set, with amount A and base amount B:

  | Holding | Line on `account` | Line on `into` |
  |---|---|---|
  | In the base currency, A | −A | +A |
  | In another currency, A ≠ 0 | −A at rate r, base −`RND-FX`(A × r) | +A at rate r, base +`RND-FX`(A × r) |
  | The base left, d = B − `RND-FX`(A × r), when not zero; all of B when A = 0 | −d in the base currency | +d in the base currency |

  r is `RND-RATE` of B ÷ A (base per one unit, the quotient rounded once, half away from zero, to 10 decimals); it is 1
  when A and B differ in sign, or when that rate is not one a stored rate can hold (0 once rounded, or 10^10 or more).
  Each line keeps the holding's dimension values; a holding whose amount and base are both 0 moves nothing.
- `account` holds nothing in any currency or dimension set after the entry, and `into` gains exactly what it held. A
  merge of an account that holds nothing posts no entry. Either way `account` is inactive from then on: a later document
  of the scenario with a line on it is rejected (rule 3 of "The rules every P1 source shares"), and so is a void or an
  edit, whatever its date, of a journal whose current entry has a line on it, or an edit whose new version has one.

The entry is in canonical form ("P1 entries"), `kind` `post`, on `date`. A merge is the system's posting: an account
that requires a dimension takes its lines without values as they come.

A merge is rejected whole when:

1. `account` and `into` are the same account, or either is not in `accounts`, is a group account, is inactive (an
   account merged before it in the scenario is), has a `role`, is named in `settings.accounts`, is a money account
   (in `settings.money_accounts`), or is marked `revalue`. The product merges a revalued account into another revalued
   one only (FR-COA-04), moving what its month-end revaluations left on it too; the format leaves such merges out for
   now, since neither side reads a month's revaluation before the scheduler runs;
2. their types differ;
3. `date` is after `as_of`, or before `settings.books_start` when it is set;
4. an entry posted before the merge (as above) has a line on `account` dated after `date`;
5. something still posts to `account` after `date` by itself: a release of a spread of a document before it in the
   scenario, an automatic reversal of a manual journal before it, or a fixed asset before it whose cost is on `account`
   and that is not disposed of on or before `date`.

### Month-end revaluation (FR-GL-04), `RND-RATE` and GST in another currency (ENG-42)

From FR-GL-04 (rev 0.8, 0.72, 0.73, 0.75, 0.92 and 0.119), INV-05, ENG-40, ENG-41, ENG-42, §10.2 ("FX revaluation
(month end)"), Appendix B.5 (rev 0.31: "7040 Unrealised FX gain or loss"), `RND-RATE` (§10.4, rev 0.24 and 0.70) and
G4-039, written for `p1-multi-currency-fx` task 6.1 from the PRD, not from the engine (QA-ENG-01). `cmd/scenario-run`
reads them (`internal/differential/p1_fx.go`, through `ledger.Revalue`): the golden case is
[`golden/fx-revaluation.json`](golden/README.md), and the generator's source `fx` is in `DIFFERENTIAL_SOURCES`. A scenario with no account marked `revalue`
and no `period_end_rates` is unchanged, and so is its result. What the PRD leaves open is in "P1 readings" (the rows
from "When a month is revalued" on).

#### The revalued accounts and the period-end rates

```text
{"code": "1021", "type": "asset", "group": false, "active": true, "revalue": true}
```

`"revalue": true` is the account's revalue flag (FR-COA-02; FR-GL-04 rev 0.72: "an account's revalue flag marks a
monetary item; only an asset or a liability may carry it, never prepayments or deferred revenue (COA-10)"). The
standard chart sets it on the money accounts, 1100, 2010, 1250, 2500 and 2120; a scenario sets it where it wants. A
flag on an account that is not an asset or a liability, or on the account `settings.accounts` names `prepayments` or
`deferred_revenue`, makes the scenario invalid (the chart refuses it). A scenario with a flagged account needs
`as_of` and `settings` whose `accounts` name `unrealised_fx` (7040), an active revenue or expense account that is not
a group and has no role ("FX-07 no unrealised-FX account" refuses a run in the product), or it is invalid.

```text
"period_end_rates": [
  {"date": "2026-03-31", "currency": "USD", "fx_rate": "1.30", "fx_rate_source": "monthly table 2026-03"},
  {"date": "2026-04-30", "currency": "JPY", "quote": "0.9012", "form": "base-per-100", "fx_rate_source": "MAS daily feed of 2026-04-30"},
  {"date": "2026-04-30", "currency": "KWD", "quote": "0.2265", "form": "foreign-per-base", "fx_rate_source": "MAS daily feed of 2026-04-30"}
]
```

`settings.period_end_rates` gives a currency's rate for a month's last day, the rate cascade's result for that day
with its source ("with the rate from the cascade (ENG-40) for the period's last day, stored with its source"): either
`fx_rate`, a stored rate (base per one unit, as a document's), or a `quote` with its `form`, which `RND-RATE` stores
(below). Each row's date is a month's last day, its currency is in `currencies` and is not the base, and a currency has
at most one row a date; a rate or a quote is a plain positive decimal with at most 10 integer digits and 10 decimals.
Anything else makes the scenario invalid: these are rates the organisation has stored.

#### `RND-RATE`

"A rate quoted as base currency per n units of the foreign currency (n = 1 or 100) is stored as round(quote ÷ n, 10);
a rate quoted as foreign currency per one unit of the base currency is stored as round(1 ÷ quote, 10); half-up" (§10.4;
rev 0.70: "the forms named base-per-unit, base-per-100 and foreign-per-base"). The quotient is exact and rounded once,
half away from zero (open question 53: "10 decimal places, half away from zero"), to 10 decimals: `base-per-100`
0.9012 is 0.009012 SGD per yen, and `foreign-per-base` 0.2265 is 4.4150110375 (1 ÷ 0.2265 = 4.41501103752...). A quote
whose stored rate is not a valid rate (0 once rounded, or 10^10 or more) makes the scenario invalid, since it could not
have been stored.

`RND-RATE` enters a scenario in two places only. A document's `fx_rate` is already the stored rate, base per one unit
(ENG-11: "a manual journal's rate is entered as base per one unit"; the cascade's result for every other document), so
neither side converts it. The two are:

1. the quotes of `period_end_rates` (the feed's and the organisation's rates are "stored this way", rev 0.70);
2. the rate a base-currency money account's payment of bills in another currency implies for what it keeps unapplied:
   `RND-RATE` of what it paid (`base_amount`) ÷ the total in the bills' currency, the parts and the unapplied amount
   together (rev 0.77, `rounding.Implied`; section "Bills ..."), round(paid ÷ total, 10). The supplier's credit is
   carried at `RND-FX` of the unapplied amount at that rate, and the revaluation reads it at that rate. A receipt into
   a base-currency account for invoices in another currency keeps its unapplied amount the same way.

#### What a month's revaluation reads

The month is revalued at its last day D. It reads the entries posted so far dated on or before D: the documents',
the reversals' and edits', and the scheduled entries (releases, depreciation, automatic reversals), but no revaluation
or its reversal: each month's revaluation is reversed on the next month's first day, on or before the next D, so
"nothing is counted twice". Each flagged account's balance in each currency other than the base is read as
**items** ("per item and currency"):

- **The receivable and payable accounts** (those `settings.accounts` names `receivable` and `payable`), when
  flagged, document by document (rev 0.92 and 0.119): each posted invoice, credit note, bill and vendor credit in that
  currency dated on or before D, and each customer's or supplier's credit (the `unapplied` amount of a receipt or a
  payment, FR-AR-18) dated on or before D. Each is at what is open of it on D, its amount less the parts settlements and
  write-offs dated on or before D settled of it, and its carrying amount, its line's base amount at its stored rate less
  the carrying amounts those settled ("each open bill and supplier credit in a foreign currency is an item, at what is
  open of it and the carrying amount the AP sub-ledger gives it at its stored rate"). An item is signed as its line on
  the account: an invoice, a vendor credit and a supplier's credit are debits; a bill, a credit note and a customer's
  credit are credits. What the account holds in that currency beyond them is **one more item**, the account's balance
  less the documents' items ("what 2010 holds beyond its documents (a manual journal, an earlier account-level line) is
  an item of its own, so 2010 ends the period at the rate"), and a settlement dated before the document it settles is
  part of it.
- **Every other flagged account** (a money account, 1250, 2500, 2120, a manual journal's account): its balance in that
  currency, the sum of its lines' amounts and base amounts, as one item; on a director's account one item for each
  person, whose line names that person (FR-EQ-01: "every journal line on 2500 or 2120 carries that contact as its
  sub-ledger key").

An item is **open** when its amount or its carrying amount is not zero: a balance of nothing in the currency that keeps
a base amount (a USD wallet that received at one rate and paid at another: USD 0.00 carried at S$5.00) is open, and is
revalued to S$0.00.

#### The revaluation entry and its reversal

A month is revalued when every currency of an open item has a period-end rate for D (rev 0.75: the scheduler runs it
"once the period has ended and every currency has a rate for its last day"). When one has none, the month's
revaluation waits and posts nothing at all (the close check, C.2 #59, names the missing rate), and the next month is
revalued on its own. A month with no open item posts nothing.

Each open item's change is `RND-FX`(its amount × the period-end rate) − its carrying amount ("unrealised = `RND-FX`(open
foreign amount × period-end rate) − carrying base"); an item already at the rate changes nothing. The entry, kind
`revaluation`, document `revaluation-YYYY-MM` (its month), dated D, posts:

- each change as a **base-only line** (INV-05: "transaction-currency amount 0, base amount not 0") on the item's account
  in the item's currency, naming the item's person on a director's account;
- `unrealised_fx` (7040), a base-currency line, the changes' sum negated: a loss is a debit and a gain a credit (§10.2:
  "Open FC items and FC bank balances, or 7040 unrealised FX loss | 7040 unrealised FX gain, or the items").

It is written in canonical form (section "P1 entries"): one line per account, currency and person, so 7040's losses
and gains are one line, their net (rev 0.72 posts one line for the losses and one for the gains), and lines that net to
nothing are left out. When every change is zero the month posts nothing ("an item already at the rate posts
nothing"). An entry that posts has at least two lines and balances in base by construction; its base-only lines
balance in their currency (every amount is 0).

"The entry reverses on day 1 of the next period": kind `revaluation_reversal`, the same document, dated the next
month's first day, negating the revaluation's lines as stored, amount and base amount (ENG-14), in its order. Every
period of a scenario is open, so it posts on that day ("at once when that period is open").

Both are scheduled entries (section "P1 entries"), and post when dated on or before `as_of`, the scheduler's one run
after every document, reversal and edit: with `as_of` on 31 March, March's revaluation posts and its reversal (1
April) waits; with `as_of` on 30 March, neither posts. Among the scheduled entries of their date they come last, after
every document's (a revaluation reads the whole day). Months are revalued from the month of the first posted entry to
the last month whose last day is on or before `as_of`.

**Settlements are unchanged.** A receipt, payment or write-off settles a document at its stored rate whatever the
revaluations before it, and 7030 takes the realised FX (ENG-41: "`RND-FX`(the amount settled × the settlement rate) −
`RND-FX`(the amount settled × the document's stored rate)") within what balances the entry (sections "Bills ..." and
"Invoices ..."): the revaluation's reversal on day 1 leaves the carrying amount at the stored rate, "so a later
settlement measures realised FX against the document's stored rate (ENG-14, ENG-41) and nothing is counted twice".
G4-039: an invoice of USD 10,000.00 at 1.35 (S$13,500.00); 31 March at 1.30: Dr 7040 500.00, Cr 1100 USD 0.00 /
S$500.00, reversed on 1 April; 15 April USD 4,000.00 received at 1.32 into the USD wallet: Dr 1021 USD 4,000.00 /
S$5,280.00, Dr 7030 120.00, Cr 1100 USD 4,000.00 / S$5,400.00; 30 April at 1.33: USD 6,000.00 carried at S$8,100.00 is
S$7,980.00, Dr 7040 120.00, Cr 1100 120.00 (and, with 1021 flagged, its USD 4,000.00 carried at S$5,280.00 is
S$5,320.00, Dr 1021 40.00, so 7040 takes the net 80.00).

#### GST on documents in another currency (ENG-42)

"GST on foreign-currency documents is reported in SGD." What each source does, all stated in its section above:

- **Purchases** (bills, vendor credits, claims' receipt lines): the input tax is "the SGD GST stated on the supplier's
  tax invoice" (`gst_base`), never a recomputation at the book rate; "the difference between the stated GST and the
  GST at the book rate posts to 7030 on the same entry" (rev 0.74: "net + stated GST − gross"), which is what 7030 takes
  to balance the entry. With no `gst_base`, "the bill still posts, the GST stays in the cost (as for code BL)" and
  nothing is claimed (the ⚠ is not in the result). G4-023: USD 1,000.00 + GST 90.00 at 1.34 stating S$121.50: Dr 6010
  1,340.00, Dr 2200 121.50, Cr 2010 1,460.60, Cr 7030 0.90.
- **Sales** (invoices, credit notes): "converted at the organisation's GST exchange-rate source ..., then `RND-FX` and
  `RND-GST-EXCL`": 2200 takes `RND-GST-EXCL` of the nets in SGD, 1100 the customer's total in the invoice's currency at
  `RND-FX`, and the cents between them go to 7030 (rev 0.116). The invoice's own rate stands for the GST
  exchange-rate source ("P1 readings").
- **Money in and out** (bank lines' parts, deposits, a disposal's proceeds): `RND-GST-INCL` of the base amount.

### P1 readings

PRD-001 rev 0.155 decided the P1 shapes the PRD had left open, on the best current recommendation
and the official guidance, because no accountant is available yet. A decision marked [accountant to
confirm] stands until the accountant reviews it; the pages that use it show "Needs additional
verification" meanwhile (FR-UX-41, the review register's accounting area). The oracle follows each
decision. Where the generator does not produce a shape, the pending golden cases do, so the first
comparison with the engine shows any difference, and the decision settles it.

| Shape | Oracle | Requirement | Generated |
|---|---|---|---|
| The currency of a foreign bank line's parts | The part's account and 2200 take base-currency lines ("the rest" of the base amount) | Rev 0.155, Appendix B.1 [accountant to confirm] | Not generated (`golden/bank.json`) |
| GST on a foreign bank line | `RND-GST-INCL` of the part's base amount | Rev 0.141 ("of its signed gross in the base currency"); rev 0.155, Appendix B.1 [accountant to confirm] | Not generated (`golden/bank.json`) |
| The currency of a transfer's fee | The paying leg's | Rev 0.155, §10.2 [accountant to confirm] | Only base-currency fees |
| The cross-currency transfer rule | Each leg at its own rate, the fee to bank charges, the rest to 7030 | Rev 0.140; rev 0.155, §10.2 (SFRS(I) 1-21) [accountant to confirm] | Generated |
| A release in the document's own month | Dated the document's day (never before the deferral it releases) | Rev 0.155, FR-GL-03 | Generated |
| A money-out part coded SR, or a money-in part coded TX or IM | GST at the code, whatever the direction | Rev 0.155, Appendix B.1 (a refund) [accountant to confirm] | Not generated |
| A bill's, a vendor credit's or a claim's receipt line coded NA or a supply code (SR, ZR, ES, OS) | Rejected whole: a purchase document takes Appendix B.6's purchase codes, and NA has no direction | FR-AP-02 ("GST input tax codes"), FR-AP-03 ("entered as a bill is"), FR-AP-17 (rev 0.155, a claim line "costed as a bill's line"), Appendix B.6 (rev 0.169) | Generated (rejected) |
| An invoice's or a credit note's line, a part-payment deposit or a disposal's proceeds received coded NA or a purchase code | Rejected whole, whatever the registration: a supply document takes Appendix B.6's supply codes | FR-AP-02 (bills the "mirror of invoices"), FR-AR-04, FR-AR-09 ("output GST when received"), FR-FA-03 ("output tax"), Appendix B.6 (rev 0.169) | Generated (rejected) |
| A disposal's proceeds received with no GST code | Rejected while the organisation is registered on the disposal's date (a taxable supply names its supply code; none would leave its output tax undeclared); while not registered, posted with no GST | FR-FA-03 (rev 0.155, "the proceeds' GST code's rate"), Appendix B.6 (rev 0.169), §10.2 "Asset disposal" | Generated (rejected while registered; posted while not) |
| Several credit notes linked to one invoice | Together they credit at most the invoice's total before the deposits it deducts | Rev 0.155, FR-AR-04 (INVOICE-15), superseding the cap per credit note [accountant to confirm] | Not generated (`golden/sales.json`) |
| A waiver, a conversion, or a repayment's settlements on one account (together), of more than the company owes the person on that director's account at the end of the document's date | Rejected whole. What is owed is the credit, positive, in base, of the lines naming the person on that account in the entries of the documents before it in the scenario dated on or before its date (a refused document leaves none; reversals come after every document, so none counts); nothing owed refuses any amount. A conversion is checked on its `amount` as entered, before `RND-SHARE` | FR-EQ-05 (rev 0.168: a settlement is drafted for at most what is owed that day, and a batch refuses a repayment of more than is owed on the payment date, PB-25) | Generated (rejected) |
| A P1 document whose lines but one, or all, round to nothing in base in canonical form (USD 0.01 deposited at 0.0001; a claim of two USD 0.01 receipts at 0.1, which owes S$0.00), or whose entry is left all in one currency without balancing in it, whatever its number of lines (a claim of USD 0.01 on each of two accounts at 0.1; a USD 0.10 invoice coded SR at 0.1, whose GST is S$0.00, leaving USD 0.11 against 0.10) | Rejected, leaving no trace in what later documents read: an entry has at least two lines, and an entry whose lines are all in one currency balances in it, as the database refuses otherwise (db/migrations/000004, "does not balance") | Rev 0.155, INV-01, INV-05 | Generated (claims and invoices at 0.1: `golden/shrunk-5fba8ee29c.json`) |
| A claim's line in another currency | Costed as a bill's (its cost in the claim's currency at the claim's rate); 2150 (or 2500 for a loan) takes what is owed in the base currency, each receipt line's net plus GST under `RND-FX` at the claim's rate, or the `paid` the claimant states for it (rev 0.169), summed, 7030 what balances; reimbursed in the base currency only, with no rate and no FX. *Superseded:* 2150 in the claim's currency, reimbursed at the cascade's rate with the difference to 7030 | FR-AP-17 (rev 0.7 "reimbursed in SGD", rev 0.155 "2150 at the claim's rate"), SFRS(I) 1-21 (a fixed SGD amount owed); the owner's decision [accountant to confirm] | Generated (a reimbursement in another currency: generated, rejected) |
| A claim larger than what is left of its advance | 1250 takes what is left, 2150 the rest | Rev 0.155, FR-AP-17 | Generated |
| `paid` per receipt line or per claim | Per receipt line: each line of a claim in another currency may state what was paid for it; a line without `paid` is owed at the claim's rate, and the claim owes the sum. A total paid for the whole claim is not in the format | FR-AP-17 rev 0.169 ("each receipt line's amount, net and GST, at the claim's rate under `RND-FX`, line by line, or the SGD the claimant states they paid"; a card statement shows each charge apart) [accountant to confirm] | Generated (on some lines of a foreign claim) |
| A tolerance on `paid` | None: any `paid` above zero is owed as stated, however far from the line at the rate, and the whole difference goes to 7030. A policy check in the product may flag a large one; the posting is the same | FR-AP-17 rev 0.169 (left to the accountant: "a tolerance on a stated SGD amount, so a reimbursement above the actual spend does not become taxable pay") [accountant to confirm] | Generated (a few cents to 3% off the line at the rate) |
| Where `paid`'s difference goes | 7030, in the claim's entry on its date, a card's spread over the claim's rate included; the line's cost stays at the claim's rate | FR-AP-17 rev 0.169 ("with the difference to 7030 when the claim posts"; left to the accountant: "whether a card's spread over the spot rate belongs in 7030") [accountant to confirm] | Generated |
| `paid` against `gst_base` or the line's GST | Not checked against either: `gst_base` sets the input tax claimed (ENG-42), `paid` what is owed, and 7030 takes both differences | FR-AP-17 rev 0.169, ENG-42 (the PRD relates neither to the other) [accountant to confirm] | Generated (`paid` beside a GST at the rate; `gst_base` in `golden/claims.json`) |
| `paid` on a base-currency claim or on a trip, not above zero, or not a valid base-currency amount | Rejected whole | FR-AP-17 rev 0.169 ("the SGD the claimant states they paid": for a claim in another currency; a trip is costed at the mileage rate, FR-AP-16), ENG-44 | Generated (rejected) |
| A claim in another currency that settles an advance | 1250 takes what is left of the advance from what the claim owes in the base currency (`paid` included), 2150 the rest. *Superseded:* refused unless the claim is in the base currency (2150 was in the claim's currency) | FR-AP-17 rev 0.155 and rev 0.169 (what is owed is a fixed SGD amount), UC-EXP-03 (Appendix J.1: IDR receipts settle an S$800 advance) [accountant to confirm, with rev 0.169] | Generated |
| GST on a disposal's proceeds received | `RND-GST-INCL` of the amount received (as for money in, B.1) | Rev 0.155, FR-FA-03 (IRAS: "based on the consideration received") [accountant to confirm] | Generated (SR while registered), and `golden/assets.json` |
| A disposal's accumulated depreciation before the books' start | Cleared with the rest: the opening balances hold it on 1590 | Appendix B.4 ("earlier depreciation is part of opening balances") | Not generated (no books' start) |
| A partial disposal, and a void of an asset | Not in the format yet | FR-FA-03, FR-FA-01 (rev 0.151) | Not generated |
| Giving an asset away | Not in the format: a deemed supply at its open market value, which Fyniq does not post in P1 | Rev 0.155, FR-FA-03 [accountant to confirm] | Not generated |
| A foreign bank line's part on an account marked revalue | Still open: the format has no revalue flag, so the oracle posts it as any part, in the base currency | Rev 0.155, Appendix B.1 (not yet decided) | Not generated |
| The tolerance of a foreign bill paid in full | 3% of what it owes at its stored rate (its 2010 line) | Rev 0.155, FR-BNK-08 [accountant to confirm] | Not generated (only base-currency bills in full) |
| The month's differences across sources | Bills' and invoices' differences share the month's cap, in absolute value | Rev 0.155, FR-BNK-08 [accountant to confirm] | Generated (always within the cap) |
| An unapplied amount from a base-currency account paying foreign bills | At the rate the payment implies (`RND-RATE` of what it paid ÷ the total) | Rev 0.77 | Generated by the `fx` source (section "Month-end revaluation"), which revalues it |
| A bill's lines in another currency | The cost lines in the bill's currency; 2200, 1210 and 7030 in the base currency | Appendix B.2 ("net in document currency"), rev 0.83 | Generated |
| An opening balance on a revenue or an expense account | Posts as any other balance: books that start part-way through a financial year carry its income and costs so far (FR-ORG-02: "first year may be short or long"; the opening balances' guide lists those accounts "if the books start part-way through a year") | FR-GL-06 rev 0.49, FR-ORG-02 [accountant to confirm] | Generated |
| A zero balance in the opening balances, or two on one account | A zero line rejects them, as a journal's does (rule 3; rev 0.66: "the content must pass the posting rules"); lines on one account and person are netted into one balance (canonical form), and the balances are rejected when fewer than two lines are left | FR-GL-06 rev 0.49 and 0.66, ENG-24 [accountant to confirm] | Generated (a zero line) |
| A director's opening balance | Names its person and origin, as every line on 2500 or 2120 does, and counts in what the company owes the person from the books' start | FR-EQ-01 rev 0.167 and 0.168 (FY250), INV-13 ("the per-person sub-ledgers equal 2500 and 2120") [accountant to confirm] | Generated (credits, the company owing the person) |
| The statements of a scenario with opening balances | Not in the format yet: a scenario with both is invalid. The opening position is not a cash flow of the books' first month, and how the statements open on it (opening cash, the equity movement's opening, the KPIs' first month) is still to decide | FR-RPT-01, FR-GL-06 (not yet decided) | Not generated (the statements' generator drops the opening balances) |
| An automatic reversal after an edit or a void | The scheduler runs after every document, reversal and edit: it reverses the journal's current entry (an edit's replacement) on its current version's `reverse_on`, and a journal voided before it has nothing to reverse | FR-GL-02 rev 0.55 ("the reversal is part of the journal as it was approved"), FR-GL-12 (an entry is reversed at most once) [accountant to confirm] | Generated |
| A void dated after the journal's `reverse_on` | Posts on its date, and no automatic reversal follows: the void was made before the scheduler ran. In the product the scheduler may have reversed it first, and the void is then refused (a second reversal) | FR-GL-02 rev 0.55, FR-GL-12 (the scenario does not say when an action is made, only in what order) [accountant to confirm] | Generated |
| An edit's replacement dated in another month than the entry it replaces | Posts: the replaced entry is reversed in its own month and the replacement in its own, both open | FR-GL-12 rev 0.58 ("both months must be open") | Generated |
| When a month is revalued | Once its last day is on or before `as_of`, as every scheduled entry, by the scheduler's one run after every document, reversal and edit: a back-dated posting is already in, so a month is revalued once, never stale. The product runs it "once the period has ended" and again when a back-dated posting makes it stale, posting "the difference since" ("the totals are the same") | FR-GL-04 rev 0.73 and 0.75, ENG-05 [accountant to confirm] | Generated |
| A month whose open item's currency has no period-end rate | Its revaluation waits and posts nothing at all, its other currencies included; the next month is revalued on its own, since each revaluation is reversed on the next month's first day | FR-GL-04 rev 0.75 ("once ... every currency has a rate for its last day"), C.2 #59 [accountant to confirm] | Generated (a month without one rate) |
| A foreign balance of nothing that keeps a base amount (USD 0.00 at S$5.00) | An open item, revalued to S$0.00: its change is minus its carrying amount | FR-GL-04 ("unrealised = `RND-FX`(open foreign amount × period-end rate) − carrying base"; rev 0.92: "so 2010 ends the period at the rate") [accountant to confirm] | Generated (a wallet paying at another rate) |
| The receivable's and payable's documents and what else the account holds | Each open document and each credit is an item at its stored rate; the rest of the account's balance in the currency (a manual journal's line, a settlement dated before its document) is one more item | FR-GL-04 rev 0.92 and 0.119, INV-07 rev 0.78 ("each posted bill dated on or before the day ..., less its allocations by then") [accountant to confirm] | Generated (manual journals on 1100 and 2010 in USD) |
| A settlement, write-off or credit dated after the month end | Not read: a document is open on D by what settled it on or before D | FR-GL-04 ("at what is open of it"), INV-07 rev 0.78 | Generated |
| 7040's lines | One line, the net of the losses and the gains, in canonical form; the base-only lines one per account, currency and person | FR-GL-04 rev 0.72 ("one line for the losses and one for the gains"), README "P1 entries" | Generated |
| A director's account in another currency | One item per person, its base-only line naming the person | FR-EQ-01 ("every journal line on 2500 or 2120 carries that contact as its sub-ledger key"), INV-13 [accountant to confirm] | Not generated (the founders' documents are in the base currency). The product revalued such an account as one balance, whose line named no one, so the database refused the whole month's revaluation (FY250); it now revalues it person by person, as this reading says (4 Nov 2026), and `cmd/scenario-run` with it |
| The statements of a scenario with a revalued account | Not in the format yet: a scenario with both is invalid. The revaluation moves cash accounts' base amounts with no cash flow, and IAS 7 shows "the effect of exchange rate changes on cash" apart from operating, investing and financing, a line FR-RPT-01's layout does not have yet | FR-RPT-01, FR-GL-04 (not yet decided) | Not generated (the statements' generator drops the flags) |
| Where `RND-RATE` enters a scenario | The quotes of `period_end_rates` and the rate a base-currency account's payment or receipt implies for its unapplied amount; a document's `fx_rate` is the stored rate already, so no quote appears on a document | `RND-RATE` (rev 0.24, 0.70), ENG-11, rev 0.77 | Generated (quotes in each form; a payment from SGD keeping an unapplied amount) |
| An average of the monthly table's duplicate rates | Not in the format: a period-end rate is one rate or one quote. The product rounds the average once under `RND-RATE` | ENG-40 rev 0.70 ("the average is rounded once under `RND-RATE`") | Not generated |
| A rate the cascade averaged, on a document | Not in the format: `fx_rate_source` is free text, so neither side knows a rate is an average or refuses one ("an average never sets a GST amount"; rev 0.74: "an average rate is refused") | ENG-40, ENG-42 rev 0.74 (not yet modelled) | Not generated |
| The GST exchange-rate source of an invoice in another currency | The invoice's own rate, until `p1-gst` adds the organisation's setting | ENG-42 rev 0.116 ("the invoice's own rate stands in for it `[verify]`") [accountant to confirm] | Generated |
| `gst_base` on a line with no GST in the bill's currency | Claims nothing: the line charges no GST, so the SGD figure is not read (it is read only beside the GST charged, as for G4-023) | ENG-42 ("the difference between the stated GST and the GST at the book rate"), FR-AP-20 [accountant to confirm] | Not generated (`gst_base` only beside `gst`) |

## Dimensions (FR-DIM-01, FR-DIM-02; PRD-001 rev 0.171, 0.172)

A scenario may declare `dimensions`, such as Department and Project, each with its values; an account may list in
`requires` the dimensions a line a person enters on it names a value of; and a manual journal's or the opening
balances' line may carry `dimensions`, a dimension's name to one of its values' codes, so a line names at most one
value of each. Names and codes match exactly, case included; `"dimensions": []` declares none and a line's
`"dimensions": {}` names none. A dimension in use with no values can be required, and then only a journal moving an
amount between values (below) posts a line on its account.

- **What a line may name.** Each value is in use and of a dimension in use (DIM-06): a line naming a value out of
  use, or a value of a dimension out of use, is rejected with its document.
- **What an account requires.** A manual journal's line on an account that `requires` a dimension in use names a
  value of it (DIM-05), unless the journal's lines on that account, in its own entry, sum to zero in the base
  currency: such a journal moves an amount between values, as a person moves an amount a posting of the system left
  without one (rev 0.172). A dimension out of use requires nothing. The opening balances are not held to it (each of
  their values is still in use, DIM-06).
- **Edits and reversals.** A manual journal's edit is held to both rules as a journal is; an edit of the opening
  balances is held to DIM-06 only, as they are. A reversal, an edit's reversal and an automatic reversal keep each
  line's values, so every dimension set of a journal nets to zero once it is reversed.
- **Other sources.** The P1 sources carry no dimension values in this format yet, so their lines post with none. A
  line a person enters on an invoice, a credit note, a bill, a vendor credit, a bank line or an expense claim is still
  held to `requires`: one on an account that requires a dimension in use, a spread line by its own account, rejects
  its document (DIM-05). Only those lines' own accounts are checked: a bank line's parts, not its money account; an
  expense claim's receipt and trip lines; never the lines the system adds (receivables, payables, GST, the holding
  accounts of a spread). The other P1 sources (receipts, payments, deposits, refunds, write-offs, transfers, the
  founders' documents and the fixed assets) and every scheduled entry (releases, depreciation, automatic reversals,
  revaluations) post without the check, as the system's own postings do.
- **The result.** A posted line with values carries them as `dimensions`; `dimension_balances` (present when the
  scenario declares dimensions) is each posting month's base-currency total of the posted lines on each account by
  their exact set of values, the set written as `Name=CODE` joined by `|` in name order, `""` for the lines with
  none. A zero total may be left out, and a comparator reads a missing month, account or set as zero. Each month's
  sets of an account add up to its `trial_balance` total.

## Statements

From `p1-reports` (task 1.5), a scenario with `"statements": true` asks both sides for the financial
statements of every month from its first posting month to its last, months without an entry included,
in the result's `statements` (`result.schema.json` has every key). They are read from the posted
entries only, written from FR-RPT-01 (rev 0.159, [accountant to confirm]), FR-COA-02, FR-GL-10, INV-10,
INV-11 and `RND-PERCENT`, with the sections of `p1-reports` design D2 and D3; `oracle/src/oracle/statements.py`
is the oracle's side. The engine's is its own `financials.Lay`, read through an in-memory store of the posted entries
(`internal/differential/statements.go`, `memstore.go`; only presentation is mapped there). The golden case is
[`golden/statements.json`](golden/README.md), the sample company's (`golden/sample-company-seed-*.json`) states them on
the standard chart, and `make differential` draws statements over generated scenarios (`statements` in
`FYNIQ_DIFFERENTIAL_SOURCES`).

**Balances within NUMERIC(20,4).** The database keeps each account's balance for a period, per currency, as
NUMERIC(20,4), so it refuses (SQLSTATE 22003, a 422) a posting that would take one to 10^16, never truncating it
(§11.3, ENG-44, INV-05; `TestAttackABalanceBeyondItsColumnRefusesThePosting` proves it). Neither side models that
refusal: scenarios without statements still reach it (two manual journals near the limit on one account in one month)
and both sides agree on a sum the product would refuse. Statements and KPIs read those balances back, so a scenario
asking for them keeps every month's balance below 10^16: while it draws one, the generator keeps a line's amount to
seven integer digits, draws no journal near the limit, and refuses a scenario unless everything it could post, in
amount and in base (amount × rate), totals below 10^15 (`strategies_statements.reach`); `scenario-run` stops with
"outside the format" (exit 1) on a hand-written scenario beyond the limit. The owner's decision with an independent advisor (4 Nov 2026): the limit belongs to
storage, which already refuses (each entry's lines are added to the period's balance in one statement), and modelling
it on both sides would add a refusal to every source, the scheduler's retries of a release or depreciation included,
for balances no company reaches.

**The chart's attributes.** An account may carry `report_group` (free text: the line it is shown under;
an account without one is shown under its code), `function` (`COGS`, `R&D`, `S&M`, `G&A` or `Other`;
read on revenue and expense accounts) and `cash_flow_line` (read on asset, liability and equity
accounts). The standard chart's cash-flow lines, and the balance-sheet section each places, only on its
own side (rev 0.159, [accountant to confirm]):

| Cash-flow line | Places | Balance-sheet section | Cash flow |
|---|---|---|---|
| Cash & cash equivalents | An asset | Current assets | The cash itself |
| Receivables, Other current assets | An asset | Current assets | Working capital |
| Capital expenditure | An asset | Non-current assets | Makes an entry investing |
| Depreciation & amortisation (accumulated depreciation) | An asset | Non-current assets | Added back |
| Payables & accruals, Payroll liabilities, Tax liabilities, Deferred revenue | A liability | Current liabilities | Working capital |
| Share application monies (rev 0.207) | A liability | Current liabilities | Makes an entry financing: money received for shares (IAS 7 para 17(a)) |
| Borrowings | A liability | Borrowings | Makes an entry financing |
| Share capital, Dividends & other equity | An equity account | Equity | Makes an entry financing |

The account with the role `retained-earnings` is in equity whatever its line. A balance-sheet account
with no cash-flow line, one outside this set, or one of the other side (a liability on Share capital, an
equity account on Cash & cash equivalents) sits in no section: it is *unplaced*.

**Profit and loss**, the month's: each figure is what it adds to profit (revenue positive, costs
negative). Each revenue and expense account goes to the first that applies: the `income-tax-expense`
account to income tax; a revenue account to other income when its function is `Other`, else to revenue;
an expense account in the report group of the depreciation account to depreciation and amortisation
(before its function: EBITDA excludes depreciation and amortisation by definition); one with function
`COGS` to cost of sales; one with function `Other` to finance costs and other expenses; any other to
operating expenses, shown by report group and by function (none counts as `Other`). The depreciation
account is the account with the role `depreciation` or, when no account has that role, the account
`settings.accounts.depreciation` names (one system role in the product); its report group, or with none
the account alone, is depreciation and amortisation, and the account itself always is. Gross profit, EBITDA, operating profit, profit before tax and net profit are the running sums
in that order. `percent_of_revenue` has each of the twelve lines under `RND-PERCENT` against revenue
(round(line ÷ |revenue| × 100, 1 decimal), half away from zero), and is absent when revenue is zero.

**Balance sheet** at the month's last day: each placed account's balance, assets debit positive,
liabilities, borrowings and equity credit positive, by section and report group. The retained-earnings
account's group also holds the profit to date (every posted revenue and expense line so far: FR-GL-10
folds earlier years into retained earnings, and FR-RPT-01 puts the year's profit inside them). A person
whose lines on the director's accounts (`director-loan`, `deferred-remuneration`) net to a debit owes the
company: with an `advances` account in the settings, their balances on those accounts show on it instead,
with no posting (FR-EQ-11, `p1-founders-money` design D14).

**Cash flow**, indirect, money in positive, on the same placement. Each of the month's entries is
*investing* when it has a line on a capital-expenditure asset, else *financing* when it has a line on an
account placed in borrowings or equity, or on a liability on Share application monies (rev 0.207), else
*operating*. An entry's cash is the sum of its lines on cash
accounts (assets on Cash & cash equivalents).

- `investing` and `financing`: their entries' cash.
- `operating`: `net_profit`; `depreciation_and_amortisation`, minus the operating entries' lines on
  accumulated depreciation; `investing_and_financing_results`, plus the revenue and expense lines of the
  investing and financing entries (taking a disposal's gain, or a cost a director paid personally, back
  out of profit); `working_capital`, minus the operating entries' lines on each working-capital line (an
  account placed in current assets other than cash, or in current liabilities, by its line);
  `other_accounts`, minus the operating entries' lines on accounts that sit in no section (design D3);
  and `total`, their sum, which always equals the operating entries' cash, so net burn is the true cash.
- `non_cash`: the investing and financing entries with no line on a cash account (FR-RPT-01: "a
  transaction that moved no money ... is left out of investing and financing and listed apart"). A
  `director_conversion`'s and a `safe_conversion`'s have the `label` "Converted to shares", a `director_waiver`'s
  "Waived" (FR-EQ-10) and a `share_issue_cash`'s "Allotted from share application monies" (rev 0.207).
- `net_change` = operating + investing + financing; `opening_cash` and `closing_cash` are the cash
  accounts' balances at the month before's end and the month's.

**Movement in equity**: `opening` (the month before's closing; nothing before the first month),
`profit` (the month's net profit), `share_capital` (shares issued for cash: the change in accounts on the
Share capital line from every entry but a conversion), `share_capital_non_cash` (a `director_conversion`'s or a
`safe_conversion`'s change on that line: shares issued for non-cash consideration, FR-EQ-10, FR-EQ-18), `other` (the change in every other equity-section account, retained earnings' own lines included) and
`closing`, the balance sheet's equity.

**Checks**: `balances`, total assets equal total liabilities and equity (INV-10, BS_Chk); `ties`,
opening cash plus the net change equals closing cash (INV-11, CF_Chk); `equity`, the movement reconciles
(BS_EqChk); `unplaced`, the balance-sheet accounts with a balance or a line in the month that sit in no
section, by code (PL_Chk, BS_Uncl). An unplaced account breaks only `balances`, and `unplaced` names it:
the cash flow keeps its movement in operating's `other_accounts`, so `ties` holds.

**A comparator** compares every figure to the cent, reading a missing report group, function or
working-capital line as zero, and the checks, `non_cash` and `percent_of_revenue` as they are.

Readings the PRD leaves open, each [accountant to confirm] with rev 0.159:

| Shape | Oracle | Why |
|---|---|---|
| Which section an entry's cash falls in | The whole entry's: a laptop's GST is investing with it, a loan's interest paid with the repayment is financing, a disposal's proceeds are investing | FR-RPT-01 classifies transactions ("a transaction that moved no money ... is left out of investing and financing"); IAS 7 and the IFRS for SMEs classify cash flows, and proceeds from a sale of equipment are investing |
| Equipment bought on account, then paid | Non-cash investing when bought; the payment is operating (it moves only payables and cash) | As `p1-reports` design D2 records it; to confirm with the accountant, since IAS 7 would show the payment as investing |
| An entry moving both capital expenditure and borrowings or equity | Investing | Investing is tested first, as D2 lists it |
| The profit to date in the balance sheet | All revenue and expense posted so far, inside the retained-earnings account's group | FR-GL-10 and FR-RPT-01; a scenario has no opening balances, so no earlier year is missing |
| A month with no entry between two that have | Stated: nothing in the P&L and the cash flow, the balance sheet as the month before | FR-RPT-02 runs any period of whole months |
| An unplaced account (D3) | `balances` stays total assets = total liabilities + equity, so it breaks; operating keeps its movement in `other_accounts`, so `ties` holds and net burn is the true cash. *Superseded:* `ties` broke too when an operating entry moved it | The owner's decision with an independent advisor (rev 0.159): the check finds the misplaced account, and the cash flow does not hide cash that moved [accountant to confirm] |
| A cash-flow line of the other side (a liability on Share capital, an equity account on Cash & cash equivalents) | Places nothing: the account is unplaced, in no section and no cash-flow role. *Superseded:* the line placed the account whatever its type | The owner's decision with an independent advisor (rev 0.159): a line names a section of its own side of the balance sheet [accountant to confirm] |
| Money received for shares not yet allotted (share application monies, 2040) | Its own line, Share application monies: a current liability, and its entries financing; the allotment that moves it into share capital is a non-cash item, "Allotted from share application monies" | PRD-001 rev 0.207: IAS 7 para 17(a) shows cash from issuing shares as financing, and burn and runway read operating cash; a line of its own keeps rev 0.159's rule that a line names a section of its own side [accountant to confirm] |
| The depreciation account when no account has the role `depreciation` | The account the settings' `depreciation` key names; its report group (or the account alone) is depreciation and amortisation, whatever the function of the accounts in it. *Superseded:* no depreciation account, so the group's accounts went by their function (one with function Other to finance costs) | The settings' key and the chart's role are one system role in the product (rev 0.159) [accountant to confirm] |
| An expense account in the depreciation account's report group whose function is COGS | Depreciation and amortisation, below EBITDA. *Superseded:* cost of sales | The owner's decision with an independent advisor (rev 0.159): EBITDA excludes depreciation and amortisation by definition [accountant to confirm] |

## KPIs

From `p1-dashboard-kpis` (task 1.3), a scenario with `statements` may also have `kpis`, which asks both
sides for every stated month's KPIs in the result's `kpis` (`result.schema.json` lists them). They are
read from the month's statements and trial balance and the entered counts, written from the workbook's
own definitions (its PRD §11.5, which FR-KPI-02 adopts), FR-KPI-02 and FR-KPI-03 (rev 0.161) and
`RND-KPI`; `oracle/src/oracle/kpis.py` is the oracle's side, the engine's own `kpi.Read` and `kpi.Compute` the
engine's, and `golden/statements.json` carries them.

```text
"kpis": {"year_start_month": 1,
         "counts": {"2026-01": {"paying_customers": 10, "new_customers": 10, "customers_lost": 0, "headcount": 3},
                    "2026-03": {"headcount": 4}}}
```

`year_start_month` is the financial year's first month; `counts` are the month's entered counts (rev
0.161: the three customer counts together, headcount on its own). Each KPI is computed from exact figures
and rounded once, half away from zero (`RND-KPI`): amounts at the base currency's minor units;
percentages and margins to 1 decimal as `RND-PERCENT` (against the size of the whole, with the part's
sign); ratios, multiples, months and days to 1 decimal; headcount whole. An undefined KPI is `null`,
never 0 (FR-KPI-03), and a comparator matches `null` only with `null`.

| KPI | The month's | Undefined (`null`) |
|---|---|---|
| `booked_mrr`, `arr` | revenue (not other income) of the postable accounts whose report group contains "subscription", any case; × 12 | no postable (not group) revenue account, not other income, in the chart whose report group contains "subscription" |
| `contracted_mrr` | always `null`: no subscription data until FR-AR-15 (P2) | always |
| `revenue_growth` | `RND-PERCENT` of (revenue − the month before's) against the month before's | the first stated month (none before it), or the month before had no revenue |
| `gross_margin`, `ebitda_margin`, `net_margin` | `RND-PERCENT` of gross profit, EBITDA, net profit against revenue (the statements' `percent_of_revenue`) | revenue 0 |
| `spend_by_function` | R&D, S&M and G&A: the expense lines of the accounts with that function, whatever their P&L section | never |
| `net_burn` | −(operating + investing cash flow) | never |
| `runway` | closing cash ÷ the three-month average net burn; `"not-burning"` when that average is not above zero | — |
| `arpa` | booked MRR ÷ paying customers | no counts, no MRR, or 0 paying |
| `churn` | `RND-PERCENT` of customers lost against customers at the start (paying − new + lost) | no counts, or none at the start |
| `cac` | S&M spend ÷ new customers | no counts, or 0 new |
| `ltv`, `ltv_to_cac`, `cac_payback` | ARPA × gross margin ÷ churn; LTV ÷ CAC; CAC ÷ (ARPA × gross margin), from the exact values | an input undefined, or a divisor 0; for `cac_payback`, ARPA × gross margin not above zero |
| `burn_multiple` | net burn ÷ net new ARR (ARR − the month before's) | the first stated month (none before it), the month's net burn not above zero, no MRR, or ARR not grown |
| `current_ratio`, `quick_ratio` | current assets ÷ current liabilities; (closing cash + the Receivables line's balance) ÷ current liabilities | current liabilities not above zero |
| `working_capital`, `debt_to_equity` | current assets − current liabilities; total liabilities ÷ equity | equity not above zero (debt/equity) |
| `dso`, `dpo` | the Receivables line's balance × 30 ÷ the average revenue; the Payables & accruals line's (credit positive) × 30 ÷ the average costs (cost of sales and operating expenses) | that average not above zero |
| `headcount`, `revenue_per_employee` | as entered; revenue × 12 ÷ headcount | not entered, or 0 (per employee) |

Three-month averages take the month and up to two before it, inside its financial year and from the
first stated month on ("the months of the selected financial year that exist"). The first stated month is
the books' first: there is no month before it, so its `revenue_growth` and `burn_multiple` are `null`.
A line's balance (Receivables, Payables & accruals) is that of the accounts it places, as the statements
place them.

Readings the PRD and the workbook leave open, each [accountant to confirm] with rev 0.161:

| Shape | Oracle | Why |
|---|---|---|
| Which accounts are subscription revenue | Revenue accounts (not other income) whose report group contains "subscription" | The workbook's "subscription revenue"; `p1-dashboard-kpis` design ("accounts whose report group is a subscription stream") |
| Customers at the start of the month | Paying − new + lost, from the month's own counts | The counts the month records; it needs no earlier month's entry |
| Spend by function | Every expense account with the function, depreciation included when its account says G&A | The workbook: "totals from each account's function" |
| The costs DPO averages | Cost of sales and operating expenses (above EBITDA) | What suppliers bill; depreciation, finance costs and tax are not bought on credit |
| "Not burning" for the burn multiple | The month's own net burn not above zero | The multiple is the month's; runway's "Not burning" is the three-month average's (FR-KPI-03) |
| A divisor not above zero | DSO and DPO `null` when the three-month average revenue or costs is not above zero; the current and quick ratios `null` when current liabilities are not above zero. *Superseded:* `null` only at exactly zero | The owner's decision (rev 0.161): a negative average or a debit on current liabilities gives no meaningful days or ratio [accountant to confirm] |
| The month before the first stated month | None: it is the books' first month, so `burn_multiple` is `null` there, as `revenue_growth` already was; the three-month averages are unchanged. *Superseded:* a month before with nothing posted, revenue and ARR 0 | The owner's decision (rev 0.161): a scenario has no opening balances, so nothing before it is known [accountant to confirm] |
| A subscription-named account whose function is Other | Not subscription revenue: with it the only one, `booked_mrr`, `arr` and `arpa` are `null` | The owner's decision (rev 0.161): booked MRR is revenue, never other income [accountant to confirm] |
| CAC payback against a margin per account not above zero | `null` when ARPA × gross margin is not above zero, as DSO and DPO. *Superseded:* `null` only at exactly zero (a CAC of 0.00 against −100% gave 0.0) | The owner's decision (rev 0.161): a payback against a negative margin means nothing [accountant to confirm] |
| A subscription-named group heading | Not subscription revenue: a heading carries no figures, so with it the only one, `booked_mrr`, `arr` and `arpa` are `null` | The owner's decision (rev 0.161): only postable accounts are streams [accountant to confirm] |

## Cap tables

From FR-EQ-20, the equity register's design D7 and Appendix G.3, a scenario may have `cap_tables`, the days to compute
the cap table at the end of; the result's `cap_tables` gives each in that order (`result.schema.json`).
`oracle/src/oracle/captable.py` is the oracle's side, the engine's `equity.TableAt` the engine's, and
`golden/cap-table.json` pins G.3's round.

```text
"cap_tables": ["2026-03-11", "2026-03-12"]
```

Holdings come only from documents that move shares, each on its date once it is not rejected: a conversion of a
founder's balance (`director_conversion`) moves the n whole shares `RND-SHARE` allots into `person`'s holding of
`class`; a cash issue (`share_issue_cash`) moves `shares` into `person`'s holding of `class`; a transfer
(`share_transfer`) moves `shares` out of the seller's holding and into the buyer's; a SAFE conversion
(`safe_conversion`) moves each instrument's n shares into its holder's holding of its class. Opening balances on share
capital name no holder, so they move no holding. A class is its share-capital account.

At the end of a day, each holding with shares above zero is a line, by holder, then class (in code-point order of
their IDs):

- `issued`: the shares held, a whole number;
- `votes`: `issued` × the class's `votes_per_share` (an attribute of the share-capital account, one when left out),
  exact, as a plain decimal with no trailing zeros ("1.5", "0");
- `fully_diluted`: the shares held as the format has them: the same as `issued`, since it counts no instrument
  before it converts (FR-EQ-18) and has no option pool (FR-EQ-19) yet;
- `ownership`, `voting` and `diluted`: the line's `issued`, `votes` and `fully_diluted` as a percentage of the day's
  totals, round(part ÷ total × 100, 2 decimals) half-up from the exact quotient, once (FR-EQ-20, which names
  `RND-DISPLAY` for it); `null` when that total is zero. They are never forced to sum to 100.00.

`holders` sums each holder's lines (`issued`, `votes`, `fully_diluted`) and takes the percentages of those exact sums,
not the sum of the rounded ones. The day's `issued`, `votes` and `fully_diluted` are the totals of every line.

G.3's round (`golden/cap-table.json`): 1,000,000 founder shares, the investor's 200,000 and 52,083 more, of 1,252,083,
are 79.87%, 15.97% and 4.16% (sum 100.00). Its 52,083 shares come from a third subscriber's cash issue at S$2.40
there; `golden/safe-conversion.json` converts the SAFEs themselves (section "SAFEs and their conversion").

Readings the PRD leaves open, each [accountant to confirm]:

| Shape | Oracle | Why |
|---|---|---|
| Votes of shares in an account with no recorded class | One vote a share (`votes_per_share` left out) | The format names classes by account; in the product such shares count under their account's first recorded class, and one vote a share while it has none (design D7) |
| Voting percentages when no share votes | `null`, as any percentage of a zero total | A percentage of nothing is undefined, never 0.00 |

## Commands

The comparison itself (QA-ENG-02): `make test` runs `TestTheEngineAgreesWithTheOracle`
(`internal/differential`), which posts every golden scenario and 60 generated with a fixed seed
through both implementations; `make differential` lets Hypothesis search 1,000 more
(`FYNIQ_DIFFERENTIAL_EXAMPLES`) and saves the smallest disagreeing scenario as
`golden/shrunk-<hash>.json`, to be committed as a golden case once the cause is fixed. It draws P1
scenarios too for the sources in `FYNIQ_DIFFERENTIAL_SOURCES` (comma-separated), which the Makefile
sets to every source `cmd/scenario-run` reads, with the statements and the KPIs:
`bank,purchases,sales,claims,assets,founders,general-ledger,fx,statements,kpis` (`make differential DIFFERENTIAL_SOURCES=founders`
draws one alone; the variable in the environment does not, since the Makefile sets it); `fx` is the month-end
revaluation, `RND-RATE`'s quotes and realised FX (section "Month-end revaluation"). The engine's
side reads scenarios with the same schemas (`schemas.go` embeds them) and runs as `cmd/scenario-run`.

```bash
uv run --project oracle python -m oracle run <scenario.json>        # result on stdout; status 2 if invalid
uv run --project oracle python -m oracle generate --seed <n> --count <n> --out <dir>   # <dir>/gen-<seed>-<i>.json
```

The same seed gives the same scenarios with the same Hypothesis version (pinned in
`oracle/uv.lock`) and the same oracle source: Hypothesis also draws on constants it finds in the
oracle's own modules, so a change to the oracle changes what a seed generates.
