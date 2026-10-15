# Proposal

## Why

PRD-001 rev 0.24 (15 Oct 2026, #27) decided open questions 47–55: the rules a P0 manual journal and its
reversal post by. The engine and the independent oracle must both follow them, and the differential gate
(QA-ENG-02) must cover them. Today the oracle's generator avoids every shape whose rule was open, and
three of the decisions change what one side or both sides do.

## What Changes

- **Manual journal validity (ENG-24, §10.5, P0).** All eight rules in one place. A journal that breaks
  any of them is rejected whole, with every failure reported at once, and the engine never adds, drops
  or changes a line.
- **Rates on lines (ENG-03's rev 0.24 note).** A base-currency line stores rate 1 with source "base",
  and a rate entered on it is refused. A converted line always has a source; a rate typed on the
  document has the source "document".
- **Input boundary (ENG-44's note).** Decimal places are counted as written (`"12345.0"` is refused for
  JPY). A computed base amount outside its column's range is refused with a field error, never truncated.
- **Zero lines (INV-05's note, ENG-24 rule 3).** A non-zero line whose base amount rounds to 0.00 posts;
  a line whose amount is zero is refused.
- **Reversals (the notes on ENG-14 and FR-GL-12, from P0 for manual journals).** A reversal negates the
  stored lines and never re-converts them. It is dated on or after its entry, in an open period, and
  each entry is reversed at most once.
- **Share capital (INV-13's note, from P0).** The chart of accounts marks share-capital accounts (3010,
  3020). A manual journal line to one is refused in the domain and by the posting-source trigger.
- **Scenario format (`fyniq-scenario@1`, `test/differential/`).**
  - Accounts gain an optional `role`, whose only value is `share-capital`.
  - `fx_rate` requires `fx_rate_source`, so a rate without its source makes the scenario invalid.
- **Oracle.**
  - It refuses earlier-dated reversals and lines to share capital.
  - The open choices listed in its README become decided.
  - Its generator makes every shape that was open, so the gate covers them.
- **Not in this change:**
  - `RND-RATE`: P1, with the rate feed (ENG-40).
  - INV-07's ⚠ for a manual journal to a control account: P1, with ENG-21.
  - FR-GL-12's edits and voids of other document types: P1.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). Rev 0.24 already put these requirements in the main specs:
`ledger/validation`, `ledger/money-and-rounding`, `ledger/posting-engine`, `ledger/general-ledger` and
`ledger/invariants`. This change builds them for P0's posting source, the manual journal.

## Impact

- **Engine (the engine session):**
  - manual journal validation;
  - base-line rates;
  - reversal rules;
  - the chart-of-accounts mark (a migration) and INV-13's trigger;
  - `cmd/scenario-run` reading the account role.
- **Oracle (the oracle session):** `oracle/` (ledger and strategies) and `test/differential/` (schema,
  README, golden cases).
- **Screens:** none added or altered. The journal screens (`journal-editor`, `journals`,
  `void-confirmation`) are P1 and were re-checked against rev 0.24 in #27.
- **Process (QA-ENG-01):** the oracle session wrote this proposal from the PRD rows and the scenario
  format; it did not read the engine. The engine session revises its half (`/opsx:update`) before
  `/opsx:apply`. Neither session reads the other's code.
