---
key: accounting.currencies
type: task
title: Exchange rates
summary: Where a posting in another currency takes its rate from, your own rates, and revaluing a month's foreign-currency balances
sources:
  - docs/user-guide/currencies.md sha256:e940bce5bb12c75839cae9fb22ef4cc7b4757ce88def57928b801171ec4bb404
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-22
aliases:
  - exchange rate
  - FX rate
  - foreign currency
  - currency rate
  - MAS rate
  - daily rate feed
  - monthly rate
  - rate override
  - no exchange rate
  - rate cascade
  - import rates
  - withdraw a rate
  - revaluation
  - revalue a month
  - unrealised FX
  - unrealised gain or loss
  - foreign currency bank account
---

# Exchange rates

## What it is

- **Accounting → Currencies** (Expert mode) keeps the rates that turn other currencies into the base currency; every rate is how much of the base currency one unit buys, kept to 10 decimal places.
- Rates come from the **daily feed**, the same for every company and published for business days only, and from **your rates**: a rate for a day, which overrides the feed's for that day, and the **monthly table**, a rate for a whole month.
- **Money accounts in other currencies** shows each bank, card, cash or wallet account's balance in another currency, in that currency and in the books in the base currency.
- The **Revaluation** tab restates each month's balances in other currencies at the month-end rate.
- An Accountant or a Controller adds and withdraws rates and revalues a month; the Owner, the Auditor and the Tax agent can see them.

(src: docs/user-guide/currencies.md#what-it-is)

## How a posting finds its rate

- A journal line in another currency with no rate of its own takes the rate for the journal's date ([[accounting.journals]]), looking in order for: your rate for that day; the daily feed's for that day, or its latest up to 7 days before (none on weekends and public holidays); your monthly rate for that month.
- When a month has more than one monthly rate, Fyniq takes their average and shows ⚠ until all but one are withdrawn; an average never sets a GST amount.
- With none of these the line cannot post: ✖ **No exchange rate for this currency** (FX-01).
- The line records the rate it took and where it came from, such as "MAS daily feed of 2026-10-16", and keeps it: a rate added or withdrawn later never changes a posted line.
- **Find the rate a posting takes** shows the rate for a currency and a date.

(src: docs/user-guide/currencies.md#how-a-posting-finds-its-rate)

## Add a rate

- **Add a rate**: choose a day or a month, the currency, the day or month and the rate, and how it is quoted (base currency for 1 unit, for 100 units, or units for 1 of the base currency); Fyniq turns it into base currency for 1 unit and keeps the quote beside it.
- A new rate for a day replaces your earlier rate for that day, which is withdrawn with the reason "Replaced by a new rate for the same day". A rate is never changed: withdraw it and add another.

(src: docs/user-guide/currencies.md#add-a-rate)

## Import rates from a file

- **Import a file** takes a CSV whose first line names the columns `currency`, `date` and `quote` (and `note`), dates written as `2026-10-16`; choose whether each rate is for its day or its month, and how the rates are quoted.
- Each rate is entered on its own: a rate that cannot be entered is listed with its line and why, and the others are kept. A file holds up to 250 rates.

(src: docs/user-guide/currencies.md#import-rates-from-a-file)

## Withdraw a rate

- **Withdraw** beside a rate, with a reason: postings no longer take it, postings that took it keep it, and **Show withdrawn rates** lists it with who withdrew it, when and why.

(src: docs/user-guide/currencies.md#withdraw-a-rate)

## Revalue a month

- At each month's end, balances in other currencies on accounts revalued at month end (such as a USD bank account or a receivable in euros) are restated at the rate for the month's last day; which accounts are revalued is set in the chart ([[accounting.chart]]).
- On the **Revaluation** tab, choose the **Month** (it starts on the latest open month that has ended), check each balance in its currency and in the books, the month-end rate and its source, and the gain or loss, with the **Posting preview** and the balances **Not revalued**; then **Revalue** with the month's name.
- The gain or loss posts on the month's last day against 7040 Unrealised FX gain or loss and reverses on the next month's first day, so a later payment measures its realised gain or loss against the invoice's own rate; if the next month is not open yet, the reversal posts once it is.
- A month is revalued from its last day while it is open. A currency with no rate for that day is named, and **Add a rate** enters it. Anything dated in the month or before posted after the revaluation means revaluing it again; the new run posts only what changed.
- The month's close refuses a month with balances to revalue that is not revalued since its last posting ([[accounting.periods]]).

(src: docs/user-guide/currencies.md#revalue-a-month)

## Common problems

- FX-01, no exchange rate for this currency: add the rate for the day or the month, or type the rate on the journal line.
- FX-02, the exchange rate is not valid: a currency other than the base, a rate above zero with up to 10 decimal places, and the first day for a monthly rate.
- FX-03, the rate is withdrawn already: nothing to do.
- FX-04, withdrawing a rate needs a reason: say why the rate is wrong or no longer used.
- STORAGE-FX-01, the exchange rate does not change that way: withdraw the rate and add another.
- FX-05, the period is not open: revalue a month while it is open, or ask for it to be reopened.
- FX-06, the period has not ended: revalue the month from its last day; until then, look at its preview.
- FX-07, no account takes unrealised FX: load 7040 Unrealised FX gain or loss from the standard chart.
- Related: closing the month ([[accounting.periods]]).

(src: docs/user-guide/currencies.md#common-problems) (src: docs/user-guide/currencies.md#related)
