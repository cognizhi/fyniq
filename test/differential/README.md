# Differential scenarios (QA-ENG-02)

The Go engine and the independent Python oracle (`oracle/`) each read the same **scenario**, post
it, and write a **result**; a comparator diffs the two results to the cent (golden rule 3). This
folder holds the contract between them (OpenSpec change `p0-python-oracle-differential`, task 2.1):

| File | What |
|---|---|
| `scenario.schema.json` | `fyniq-scenario@1`: what both sides read |
| `result.schema.json` | `fyniq-scenario-result@1`: what both sides write |
| this README | The rules a JSON Schema cannot state, and how to compare two results |

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
  decimals, positive) and `fx_rate_source` are present only on lines not in the base currency.
- Documents post in array order; then each reversal reverses the posted entry of its document on
  its date. Every date falls in a calendar-month period, and all periods are open.

### Invalid scenario or rejected document

There are two kinds of failure, and they must not be confused:

**The scenario is invalid** (the oracle exits with status 2 and writes nothing to stdout; the engine's
runner should fail the same way). The input is not a `fyniq-scenario@1` document:

- it breaks `scenario.schema.json` (a missing or extra key, a wrong type, a JSON number where a string
  is required, a document type other than `manual_journal`, more than 4 minor units, an ID with `/`);
- a name, ID, account code or currency code does not match its pattern as a whole string (Python's
  `re` lets `$` match before a final newline, so the oracle checks `"JNL-0001\n"` again; RE2 refuses it);
- a JSON object repeats a key, or the JSON holds `NaN` or `Infinity`;
- `base_currency` is not a key of `currencies`;
- two accounts share a code, or two documents share an ID;
- a date is not a real calendar date (`2026-02-30`).

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
5. Every line not in the base currency has `fx_rate` and `fx_rate_source`; a base-currency line has
   neither.
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

## Choices where PRD-001 is silent

The oracle makes these choices. The generator does **not** produce the ambiguous shapes (marked
"not generated"), so a differential run does not hinge on them until the PRD settles them.

| Shape | Oracle | Generated |
|---|---|---|
| A rate (or rate source) on a base-currency line | Rejected (the format says they are present only for other currencies) | Not generated |
| A rate without `fx_rate_source` | Rejected (ENG-13: a conversion needs a rate and its source) | Not generated |
| A non-zero foreign line whose base amount rounds to zero (JPY 1 × 0.001) | Posts; the base amount is `0.00` (rule 3 is about the line's own currency) | Not generated |
| A computed base amount of 17 or more integer digits | Rejected (it does not fit NUMERIC(20,4)) | Not generated |
| A reversal dated before its document | Posts on its own date (every period is open) | Not generated |
| An amount written with trailing zeros beyond the minor units (`"12345.0"` for JPY) | Rejected (ENG-44 counts the decimal places as written) | Generated |
| A single-currency foreign journal that balances in its currency but not in base after `RND-FX` | Rejected (a manual journal has no rounding line) | Generated |
| `version` | Read but not used (P0 posts version 1 of each journal) | Always 1 |
| `fx_rate_source` values | Any non-empty string; not used in the result | Always `"document"` |

## Commands

The comparison itself (QA-ENG-02): `make test` runs `TestTheEngineAgreesWithTheOracle`
(`internal/differential`), which posts every golden scenario and 60 generated with a fixed seed
through both implementations; `make differential` lets Hypothesis search 1,000 more
(`FYNIQ_DIFFERENTIAL_EXAMPLES`) and saves the smallest disagreeing scenario as
`golden/shrunk-<hash>.json`, to be committed as a golden case once the cause is fixed. The engine's
side reads scenarios with the same schemas (`schemas.go` embeds them) and runs as `cmd/scenario-run`.

```bash
uv run --project oracle python -m oracle run <scenario.json>        # result on stdout; status 2 if invalid
uv run --project oracle python -m oracle generate --seed <n> --count <n> --out <dir>   # <dir>/gen-<seed>-<i>.json
```

The same seed gives the same scenarios with the same Hypothesis version (pinned in
`oracle/uv.lock`).
