# Proposal

## Why

The ledger already records foreign currencies: every journal line carries its currency, its rate into
the base currency and where the rate came from, and the database checks the base amount under
`RND-FX` (INV-05). But a person must type every rate, nothing keeps rates, and nothing revalues a
foreign-currency balance at month end. A USD bank account's SGD value stays at the rates of the day
money moved, so the balance sheet is wrong after any rate move, and the close cannot say so. Invoices,
bills and bank import (`p1-sales`, `p1-purchases`, `p1-bank-import-reconcile`) all post in foreign
currencies and need the rate cascade, realised FX and the GST rules first. It is the second change of
wave 2 in the P1–P3 build plan (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`), after
`p1-general-ledger-close`.

## What Changes

- **Exchange rates (FR-GL-05, `RND-RATE`):**
  - a daily rate feed behind a port, loaded by a platform job; locally from a rates file, the live
    source at the cloud gate;
  - the organisation's own rates: a day's rate that overrides the feed, and the monthly table,
    entered or imported from a file, withdrawn but never changed, and audited;
  - every rate stored as base currency per one unit, converted from the quote under `RND-RATE`, with
    the quote and its form kept beside it.
- **The rate cascade (ENG-40):** the document's own rate, then the organisation's rate for the day,
  then the feed's, then the monthly table (the average of duplicates, with ⚠), otherwise
  "✖ No exchange rate for this currency". The source is recorded on every converted amount.
- **Manual journals take the cascade:** a foreign-currency line without a typed rate takes the
  cascade's rate when it is checked and posted.
- **Month-end revaluation (FR-GL-04, ENG-41, INV-05):**
  - accounts flagged as monetary foreign-currency items;
  - base-only lines, accepted by the database only from the revaluation and its reversal;
  - the revaluation of a month, previewed and posted with its reversal on day 1 of the next period,
    run again as a reversal and replacement when a later posting makes it stale;
  - run by the scheduler and by a person;
  - a close check for a month not revalued.
- **Realised FX and GST on foreign-currency documents (ENG-41, ENG-42):** the engine's rules and
  their tests, which `p1-sales`, `p1-purchases` and `p1-bank-import-reconcile` call when their
  documents settle and post.
- **Foreign-currency money accounts (ENG-43):** both the currency and the base balance shown;
  reconciliation in the account currency comes with bank import.
- **The screen** `fx-revaluation` (Accounting › Currencies): rates, a rate look-up, the month's
  revaluation and the foreign-currency money accounts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `ledger/general-ledger`: FR-GL-04 and FR-GL-05;
- `ledger/money-and-rounding`: `RND-RATE`;
- `ledger/multi-currency`: ENG-40, ENG-41, ENG-42 and ENG-43;
- `ledger/posting-rules`: "FX revaluation (month end)" (the unnumbered row).

A decision that changes a requirement's text (see `design.md`) goes through a PRD revision and the
main specs in the pull request that builds it, as `p1-general-ledger-close` did.

## Before it starts

The plan says a change starts once every change in its "After" column is done.
`p1-general-ledger-close` is built except for tasks that wait on other changes (its `tasks.md`). This
change needs only what is built: periods and their close, the scheduler's platform jobs, automatic
reversals, the balance service and the journal's statuses.

What it cannot finish alone stays open here, named in `tasks.md`:
- revaluation of AR and AP **documents** and unapplied credits, item by item: until invoices and bills
  exist, it revalues the AR and AP control accounts by currency;
- realised FX and GST on documents, posted: `p1-sales`, `p1-purchases`, `p1-bank-import-reconcile`;
- reconciliation in the account currency: `p1-bank-import-reconcile`;
- the live rate feed: the cloud gate, once MAS confirms that the rates may be redistributed [verify];
- the duplicate-rate check C.1 #9 in the checks catalogue: `p1-checks`. Until then the page and the
  close show it.

Question 32 (settlement tolerances) is the plan's outside dependency for this change. Tolerances apply
to the residual after realised FX, which `p1-sales`, `p1-purchases` and bank import compute. This
change computes realised FX exactly, with no tolerance (FR-BNK-08), so it does not wait for the
answer.

## Impact

- **Code:**
  - `internal/domain/fx`: rates, the cascade and the revaluation;
  - `RND-RATE` in `internal/domain/rounding`;
  - a rate-feed port with a local adapter;
  - migrations for the rates, the monetary flag, the base-only lines and the revaluation records;
  - an FX API: rates, look-up, revaluation;
  - a platform job for the feed and one for the revaluation;
  - a close check.
- **Screens:**
  - Accounting › Currencies (`fx-revaluation`);
  - the journal editor's rate field;
  - the close page's check.
- **Oracle:** base-only revaluation lines, their reversal, realised FX, the ENG-42 GST difference and
  `RND-RATE` are new shapes. The oracle's author must not read the engine (QA-ENG-01), so each is a
  task for the oracle session. This change records them and does not write them.
- **Reviewer:** the accountant, for which accounts are monetary, the rate look-back and the
  revaluation.
