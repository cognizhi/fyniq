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
- An account may carry a `role`. The only role is `share-capital`, for the accounts a manual
  journal may not post to (INV-13; 3010 and 3020 in the default chart). Until both sides refuse
  those lines (change `p0-manual-journal-rules`), no golden or generated scenario carries a role.
- Documents post in array order; then each reversal reverses the posted entry of its document on
  its date. Every date falls in a calendar-month period, and all periods are open.

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
  before a date in the scenario, a reversal of a P1 document, a settings account not in `accounts`).

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
4. Every account exists, is active and is not a group account (INV-04).
5. Every line not in the base currency has `fx_rate` (and so its source); a base-currency line has
   neither `fx_rate` nor `fx_rate_source` (ENG-03).
6. Every base amount has fewer than 17 integer digits (NUMERIC(20,4)).
7. The base amounts, each computed by `RND-FX` (round(amount × rate, the base currency's minor units),
   half away from zero), sum to zero. A base-currency line's base amount is its amount.
8. When every line shares one currency, the amounts also sum to zero in that currency.

A reversal is rejected when its document is unknown or was rejected, or when the document was
already reversed (a document is reversed at most once). A posted reversal negates every line of
the original entry as stored (amount and base amount; never re-converted, ENG-14), on its own date.

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

- `entries`: the posted documents in scenario order, then the posted reversals in scenario order;
  lines in the document's order.
- Every amount is a plain decimal string at exactly its currency's minor units (`"100.00"`,
  `"12345"`, `"2.263"`); a base amount at the base currency's. Zero is `"0.00"`, never `"-0.00"`.
- `rejected`: the refused documents (`"JNL-0003"`) and reversals (`"JNL-0001/reversal"`) in the
  order they were refused, documents first. A key can repeat (a third reversal of one document).
  No reason codes, so neither side depends on the other's codes; `python -m oracle run --explain`
  prints the oracle's reasons on stderr for people.
- `trial_balance`: each posting month (`YYYY-MM`) maps each account to its base-currency total for
  that month. The oracle leaves out accounts whose total is zero and keeps a month in which every
  account nets to zero (`{}`). **A comparator reads a missing account, or a missing month, as zero.**

## Decided in PRD-001 rev 0.24

These shapes were open until PRD-001 rev 0.24 decided them (ENG-24, the manual journal's validity
rules, and the rows each cites below); both sides follow the decision. The generator does not yet
produce those marked "not generated" (task 3.4 of `p0-manual-journal-rules` adds them), so a
differential run reaches them only through golden cases.

| Shape | Both sides | Requirement | Generated |
|---|---|---|---|
| A rate (or rate source) on a base-currency line | Rejected | ENG-03 (a rate entered on a base-currency line is refused); ENG-24 rule 5 | Not generated |
| A non-zero foreign line whose base amount rounds to zero (JPY 1 × 0.001) | Posts; the base amount is `0.00` | ENG-24 rule 3; INV-05 | Not generated |
| A computed base amount of 17 or more integer digits | Rejected (it does not fit NUMERIC(20,4)), never truncated | ENG-44; ENG-24 rule 6 | Not generated |
| A reversal dated before its document | Rejected; the document is not reversed, so a later reversal still posts | FR-GL-12 ("dated on or after the entry's posting date") | Not generated |
| A line to an account with the role `share-capital` | Rejected | INV-13; ENG-24 ("the invariants of §10.7 apply as well") | Not generated |
| An amount written with trailing zeros beyond the minor units (`"12345.0"` for JPY) | Rejected | ENG-44 (decimal places counted as written) | Generated |
| A single-currency foreign journal that balances in its currency but not in base after `RND-FX` | Rejected (a manual journal has no rounding line) | ENG-24 rule 7 ("the engine never adds, drops or changes a line") | Generated |
| `version` | Read but not used (P0 posts version 1 of each journal) | ENG-03 (an entry names its document's version) | Always 1 |
| `fx_rate_source` values | Any non-empty string; not used in the result | ENG-03, ENG-40 (a rate typed on the document has the source "document") | Always `"document"` |

## P1 sources (pending: `cmd/scenario-run` does not read them yet)

From P1 a scenario may hold documents of other sources beside manual journals, each posted by its
rule set (PRD-001 §10.2, Appendix B; the oracle's `oracle/src/oracle/sources/`). The format grows
additively, still `fyniq-scenario@1`: a scenario of manual journals only is unchanged, and so is its
result. **The engine's `cmd/scenario-run` does not read these shapes yet**, so the Go gate and
`make differential` do not compare them: their golden cases wait in [`pending/`](pending/README.md)
with their figures worked out by hand, and `make differential` draws them only for the sources named
in `FYNIQ_DIFFERENTIAL_SOURCES`. Teaching `cmd/scenario-run` each source is an engine-session task in
its OpenSpec change.

### Settings and `as_of`

A scenario with a P1 document must have `settings` and `as_of`, or it is invalid:

- `as_of` is the organisation's business date ("today", ENG-05). It is on or after every document's
  and reversal's date. Scheduled entries (a spread's releases) post when dated on or before it; later
  ones are the future and never post.
- `settings.accounts` names the system accounts by key (Appendix B.5): `receivable` (1100), `payable`
  (2010), `gst` (2200), `prepayments` (1210), `deferred_revenue` (2400), `fx` (7030), `bank_charges`
  (6460, a setting), `withholding_tax` (2330), `customer_deposits` (2410). Each named account must be in
  `accounts`. A document that needs a key the organisation has not set is rejected.
- `settings.tolerance` holds the settlement tolerances of FR-BNK-08 (open question 32), each a plain
  decimal: `base` (S$1.00), `percent` (3), `per_document` (S$50.00) and `per_month` (S$200.00); a key
  left out takes that default.
- `settings.money_accounts` maps each bank, card, wallet or cash account to the currency it holds
  (FR-BNK-01); each must be in `accounts` and its currency in `currencies`.
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
   share-capital account (INV-13: only the register of share issues posts there, rev 0.49).
4. A GST code is one of Appendix B.6's twelve. GST is charged when the organisation is registered on
   the document's date (its tax point) and the code is standard-rated (SR, TX, IM), at the rate in force
   then (a registered date with no rate in force rejects the document); otherwise it is 0.
5. A spread is 1 to 120 months (FR-GL-03); its deferral account takes the whole amount when the
   document posts, and each month's `ALLOC-EVEN` part is released to the category, dated the first day
   of its month or, for a month that starts before the document, the document's date. A month whose
   part is zero releases nothing.
6. Every base amount fits NUMERIC(20,4).

### P1 entries

A P1 document posts at most one entry, `kind` `post`, on its date. To keep the comparison independent of
how each side splits a leg into lines, a P1 entry is written in its **canonical form**: one line per
(account, currency), whose amount and base amount are the sums of that account's lines in that
currency, lines whose sums are both zero left out, sorted by account and then currency. A manual
journal's entries keep their lines in the journal's order, as before.

Scheduled entries come after every document and reversal, `kind` `release`: one entry per document and
date, in canonical form, ordered by date, then by the document's place in the scenario.

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
amount and a quantity, its total is not positive, a spread is not on an expense with a cost, or
`gst_base` is on a base-currency bill.

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
date.

A `customer_receipt` is a settlement (the shape of a payment of bills) of posted invoices on 1100,
mirrored: 1100 is credited, the money account debited, a part in full is a loss on 7030, and `unapplied`
stays a credit with the customer. A `write_off` (`invoice`, `amount` at most what is open, `account` the
reason's) credits 1100 at the invoice's stored rate (all that is left of its line when it takes all that
is open) and debits the account with that carrying amount, in the base currency.

### P1 readings

Where PRD-001 leaves a P1 shape open, the oracle reads it as below. The generator does not produce
these shapes; the pending golden cases do, so the first comparison with the engine shows any
difference, which the PRD then settles.

| Shape | Oracle | Requirement | Generated |
|---|---|---|---|
| The currency of a foreign bank line's parts | The part's account and 2200 take base-currency lines ("the rest" of the base amount) | Rev 0.141 | Not generated (`pending/bank.json`) |
| GST on a foreign bank line | `RND-GST-INCL` of the part's base amount | Rev 0.141 ("of its signed gross in the base currency") | Not generated (`pending/bank.json`) |
| The currency of a transfer's fee | The paying leg's | Rev 0.140 | Only base-currency fees |
| The cross-currency transfer rule | Each leg at its own rate, the fee to bank charges, the rest to 7030 | Rev 0.140; the accountant has not confirmed it yet | Generated |
| A release in the document's own month | Dated the document's day (never before the deferral it releases) | FR-GL-03 (rev 0.83: "the bill's date for months before it") | Generated |
| A money-out part coded SR, or a money-in part coded TX or IM | GST at the code, whatever the direction | Appendix B.1 | Not generated |
| Several credit notes linked to one invoice | Each credits at most the invoice's total on its own; they are not added up | FR-AR-04 (INVOICE-15: "credits at most that invoice's total") | Not generated (only standalone credit notes) |
| A P1 document whose lines but one round to nothing in base (USD 0.01 deposited at 0.0001) | Rejected: an entry has at least two lines | INV-05 (rev 0.24 settles it for a journal's line, not for a P1 entry) | Not generated (rates of at least 0.01) |
| The tolerance of a foreign bill paid in full | 3% of what it owes at its stored rate (its 2010 line) | FR-BNK-08 (rev 0.121: "3% of the document's total at its stored rate") | Not generated (only base-currency bills in full) |
| The month's differences across sources | Bills' and invoices' differences share the month's cap, in absolute value | FR-BNK-08 ("with the month's earlier differences") | Generated (always within the cap) |
| An unapplied amount from a base-currency account paying foreign bills | At the rate the payment implies (`RND-RATE` of what it paid ÷ the total) | Rev 0.77 | Not generated |
| A bill's lines in another currency | The cost lines in the bill's currency; 2200, 1210 and 7030 in the base currency | Appendix B.2 ("net in document currency"), rev 0.83 | Generated |

## Commands

The comparison itself (QA-ENG-02): `make test` runs `TestTheEngineAgreesWithTheOracle`
(`internal/differential`), which posts every golden scenario and 60 generated with a fixed seed
through both implementations; `make differential` lets Hypothesis search 1,000 more
(`FYNIQ_DIFFERENTIAL_EXAMPLES`) and saves the smallest disagreeing scenario as
`golden/shrunk-<hash>.json`, to be committed as a golden case once the cause is fixed. It draws P1
scenarios too for the sources in `FYNIQ_DIFFERENTIAL_SOURCES` (comma-separated: `bank`), once
`cmd/scenario-run` reads them. The engine's
side reads scenarios with the same schemas (`schemas.go` embeds them) and runs as `cmd/scenario-run`.

```bash
uv run --project oracle python -m oracle run <scenario.json>        # result on stdout; status 2 if invalid
uv run --project oracle python -m oracle generate --seed <n> --count <n> --out <dir>   # <dir>/gen-<seed>-<i>.json
```

The same seed gives the same scenarios with the same Hypothesis version (pinned in
`oracle/uv.lock`) and the same oracle source: Hypothesis also draws on constants it finds in the
oracle's own modules, so a change to the oracle changes what a seed generates.
