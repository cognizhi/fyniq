# Design

## Context

- A journal line stores its currency amount, `fx_rate` (base per one unit of the line's currency,
  `NUMERIC(20,10)`), `fx_rate_source` and `base_amount`. A CHECK makes `base_amount` equal
  `round(amount × fx_rate)` at the base currency's minor units (INV-05). Its comment already reserves
  base-only lines for the revaluation.
- The engine (`internal/domain/ledger`) is a pure function. A foreign-currency line without a rate is
  refused (`CodeMissingRate`). A base-currency line takes `money.Identity` with source "base".
- `money.ParseRate` reads a rate strictly (ENG-44). `rounding.FX` is `RND-FX`.
- Accounts have `Currency` (a bank account's currency), `MoneyAccount`, `Control` and a role.
  `unrealised-fx` (7040) and `fx-rounding` (7030) are system roles in the standard chart.
- The scheduler's platform jobs run from the tick (`internal/wiring/jobs.go`). Automatic reversals
  post on their date, and wait while that month is not open (rev 0.55, rev 0.59).
- The close runs its checks under the ledger lock and records the revision they saw (ENG-46).

## Goals / Non-Goals

**Goals:**
- Rates come from a feed and the organisation's own tables, and the cascade picks one with its source.
- A manual journal in a foreign currency needs no typed rate.
- At each month end, the foreign-currency balances on monetary accounts are revalued at the month-end
  rate and reversed on day 1, and the close refuses a month that has not been revalued.
- Realised FX and GST on foreign-currency documents are engine rules that the document changes call.

**Non-Goals:**
- Invoices, bills, payments and their settlements (`p1-sales`, `p1-purchases`,
  `p1-bank-import-reconcile`).
- Cross rates for a base currency other than SGD. The feed quotes SGD, and every organisation in P1
  keeps its books in SGD.
- Hedging and forward contracts.
- Revaluing non-monetary items, ever.

## Decisions

All of these are provisional, on the recommended option, until the accountant's sign-off. Each one that
changes a requirement's text goes into a PRD revision with the slice that builds it.

- **Rates are stored one way: base currency per one unit (`RND-RATE`, question 53).**
  - A quote per 100 units is divided by 100; a quote of foreign currency per one SGD is inverted.
    Both are rounded to 10 places, half away from zero.
  - The quote, its form (`base-per-unit`, `base-per-100`, `foreign-per-base`) and its source are kept
    beside the stored rate.
- **Three kinds of rate:**
  - **feed rates**, platform-wide, one per date and currency, never changed. A global table like
    `currency`, read by every organisation and written only by the feed job;
  - **the organisation's rate for a day**: it overrides the feed for that date and currency. At most
    one is current; entering another withdraws the earlier one;
  - **the monthly table**: the organisation's rates for a month and currency. Duplicates are allowed,
    as in the workbook, and averaged with ⚠.
  - An organisation's rate is never changed or deleted. It is withdrawn, with who did it and why, and
    the rows and their audit events stay (FR-GL-05's audit).
- **The cascade (ENG-40)**, for a currency and a date:
  1. the document's own rate (for a manual journal, the rate typed on the line);
  2. the organisation's rate for the day;
  3. the feed's rate for the day, **or the latest feed rate up to 7 days earlier**. The feed has no
     rate for weekends and public holidays, and a Saturday document would otherwise fall to the
     monthly table or fail. The source records the date the rate is from ("MAS daily feed, 2026-10-16"
     [verify]) [accountant to confirm the look-back];
  4. the monthly table for the date's month: one rate, or the average of the duplicates, rounded
     under `RND-RATE`, with the source "monthly table 2026-10, average of 2 entries";
  5. otherwise ✖ `FX-01` "No exchange rate for USD on 17 Oct 2026".
  - An average never sets a GST amount (ENG-42): the cascade returns whether its rate is an average,
    and the GST rules refuse one.
  - Alternative: the feed for the exact date only. Rejected because every weekend document would need
    a monthly-table rate.
- **The feed is a port (ADR-T8).**
  - `RateFeed.Fetch(ctx, date)` returns the quotes for a date.
  - The local adapter reads `<data>/fx/<date>.csv`; tests use a fixture.
  - The production adapter (MAS's API catalogue) arrives at the cloud gate once MAS confirms
    redistribution (FR-GL-05 [verify]).
  - The platform job `fx.feed` runs daily and stores what it fetched for the last 7 days that are
    missing, so a missed day catches up.
- **Manual journals take the cascade.** A foreign-currency line without a typed rate takes the
  cascade's rate (steps 2 to 5) for the journal's date, both when its statuses are worked out and when
  it posts. The rate is fixed at posting (ENG-14). A typed rate stays the document's own rate, with
  the source the person gave it. The engine stays pure: the journal service fills the rate in before
  the engine sees the line.
- **The monetary flag (FR-GL-04, INV-05).**
  - An account attribute `revalue`: its foreign-currency balances are revalued at month end.
  - It is allowed on asset and liability accounts only, and refused on accounts with the
    `prepayments` or `deferred-revenue` role (1210, 2400).
  - The standard chart sets it on the money accounts, 1100, 2010, 1250, 2500 and 2120. 2700 is off
    until the accountant classifies the instrument as a liability (FR-EQ-18) [accountant to confirm
    per account].
  - Changing it is a chart change with its audit event.
- **Base-only lines in the database (INV-05).**
  - A base-only line has a foreign currency, amount 0 and base amount not 0. It keeps the rate it was
    revalued at and that rate's source.
  - The `RND-FX` CHECK is replaced, NOT VALID then validated, by one that also accepts amount 0 with
    any base amount.
  - A deferred constraint trigger accepts a base-only line only:
    - when the entry's source type is `fx_revaluation`, or the entry reverses one;
    - on an account with `revalue` set;
    - in a foreign currency;
    - when the entry's base-only lines are offset by base-currency lines on the account with the
      `unrealised-fx` role.
  - Anything else is refused with a stable code, by any role.
- **The revaluation of a month (FR-GL-04).**
  - For each account with `revalue`, and each foreign currency with a balance at the period end:
    unrealised = `RND-FX`(the open currency amount × the period-end rate) − the carrying base. The
    carrying base is the base balance at the period end, which includes the reversal of the previous
    month's revaluation.
  - The period-end rate comes from the cascade (steps 2 to 5) for the period's last day.
  - One entry, dated the period's last day:
    - a base-only line per account and currency with a difference;
    - the balancing line on 7040, in the base currency.
  - The entry reverses on the first day of the next period. The reversal posts with it when that period
    is open, or waits like an automatic reversal and posts when it opens.
  - Until invoices and bills exist, the AR and AP control accounts are revalued by currency. `p1-sales`
    and `p1-purchases` replace those lines with one per open document and unapplied credit, against
    the document's stored rate.
  - **A revaluation document** per period, source type `fx_revaluation`, records:
    - the rates and their sources;
    - the ledger revision it read;
    - the entry and its reversal.
  - **Stale:** a posting dated on or before the period end with a later revision than the
    revaluation's, other than a revaluation's own, makes it stale. A run of a stale revaluation reverses
    the earlier entry and posts the replacement (FR-GL-12), dated as the original.
  - **Who runs it:**
    - the scheduler: a platform job `fx.revalue`, hourly, revalues each open period whose last day has
      passed in the organisation's time zone and which has no current revaluation, under the system
      principal (FR-AUT-01, FR-AUT-02). It skips a period it has no rate for and reports it;
    - a person with C on "Period close / reopen": previews a run at any time and posts one from the
      period's last day.
  - Alternative: a person only. Rejected because FR-AUT-01 lists revaluation among the scheduler's
    jobs, and a back-dated bill would otherwise leave the month stale until someone notices.
- **The close check:** ✖ "foreign-currency balances not revalued" when a period has a foreign-currency
  balance on an account with `revalue` and no current revaluation. The check names the currency
  without a rate, if that is why.
- **Realised FX (ENG-41)** = the currency amount settled × (the settlement rate − the document's stored
  rate), exact, under `RND-FX`, to 7030, with no tolerance (FR-BNK-08). It is an engine function with
  G4-039's receipt as its test. The settlements that call it come with the document changes.
- **GST on foreign-currency documents (ENG-42)** are engine functions:
  - sales: converted at the organisation's GST exchange-rate source, then `RND-FX` and
    `RND-GST-EXCL`;
  - purchases: the SGD GST stated on the supplier's tax invoice, with the difference from the book rate
    to 7030 on the same entry, or no stated GST, which leaves the GST in the cost with ⚠.
  - G4-023 is its test. The setting for the GST exchange-rate source comes with `p1-sales`, its first
    user.
- **Rights:** V on "Period close / reopen" to read rates and revaluations; C to enter, import and
  withdraw rates and to run a revaluation (the Accountant and the Controller). The feed job and the
  scheduler run as platform jobs.

## Ports and adapters (ADR-T8)

A new port, `RateFeed`, with a local adapter (a directory of rates files) and a test fixture. The MAS
adapter is a cloud-gate task.

## Audit events (AUD-01)

- an organisation's rate entered, imported or withdrawn;
- the feed's rates stored (a platform event per run);
- an account's `revalue` changed (the chart's own event);
- a revaluation previewed (no event), posted, reversed, or run again.

## Risks / Trade-offs

- [The look-back hides a missing feed day] → The source names the rate's own date, so an old rate shows
  on the line, and a week without feed rates falls to the monthly table or fails.
- [Revaluing the control accounts by currency is wrong once documents carry their own rates] → The
  document changes replace those lines with per-document ones. Until then, nothing posts to 1100 or
  2010 except manual journals.
- [A base-only line is a hole in INV-05] → One source may write it, on flagged accounts, offset by
  7040, checked by the database for every role, with attack tests.
- [The oracle lags the engine on the new shapes] → Each shape is a task for the oracle session.
