---
key: accounting.currencies
type: task
title: Exchange rates
summary: Where a posting in another currency takes its rate from, and adding, importing and withdrawing your own rates
sources:
  - docs/user-guide/currencies.md sha256:ef586a12aeef8785cc5638f85cdb129219eef089c75648870cb83983cce7a68c
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
---

# Exchange rates

## What it is

- **Accounting → Currencies** (Expert mode) keeps the rates that turn other currencies into the base currency; every rate is how much of the base currency one unit buys, kept to 10 decimal places.
- Rates come from the **daily feed**, the same for every company and published for business days only, and from **your rates**: a rate for a day, which overrides the feed's for that day, and the **monthly table**, a rate for a whole month.
- An Accountant or a Controller adds and withdraws rates; the Owner, the Auditor and the Tax agent can see them.

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

## Common problems

- FX-01, no exchange rate for this currency: add the rate for the day or the month, or type the rate on the journal line.
- FX-02, the exchange rate is not valid: a currency other than the base, a rate above zero with up to 10 decimal places, and the first day for a monthly rate.
- FX-03, the rate is withdrawn already: nothing to do.
- FX-04, withdrawing a rate needs a reason: say why the rate is wrong or no longer used.
- STORAGE-FX-01, the exchange rate does not change that way: withdraw the rate and add another.
- Related: closing the month ([[accounting.periods]]).

(src: docs/user-guide/currencies.md#common-problems) (src: docs/user-guide/currencies.md#related)
