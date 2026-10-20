---
key: accounting.periods
title: Open the financial year and close the periods
mode: expert
roles: [accountant, controller, owner, auditor, tax_agent]
requirements: [FR-ORG-02, FR-GL-08]
status: draft            # written with the periods page (P1, p1-general-ledger-close); the close joins it
---

# Open the financial year and close the periods

## What it is

**Accounting → Periods and close** shows the company's accounting periods, a financial year at a
time. Each period is a calendar month. A journal or document posts in the month of its date, and
only while that month is **Open**. Nothing posts on a date that no open period covers.

The first financial year runs from the day the books start to the end of the first year, so its
first period is short when the books start mid-month. Each later year is the twelve months after
the one before.

An Accountant or a Controller opens a year. The Owner, the Auditor and the Tax agent can see the
periods. The page is in Expert mode.

## Open a financial year

1. Make sure **Settings → Organisation** has when the books start and when the first financial
   year ends.
2. Open **Accounting → Periods and close**.
3. Select **Open the year**, then **Open the year** again to confirm.

All the year's periods open at once. The next year can open once the year before it has started.

Once a period exists, when the books start, the month the year starts in and the end of the first
year no longer change.

## Closing a period

Closing a month, with its checks and a second founder's attestation, joins this page soon. A
closed period refuses every posting dated in it.

## Common problems

| Message | What to do |
|---|---|
| The financial years are not set (PERIOD-01) | Enter when the books start and when the first year ends in **Settings → Organisation** |
| The year starts too far ahead (PERIOD-02) | Open it once the year before it has started |
| Not the next year to open (PERIOD-03) | Reload the page: someone else opened that year |
| The financial years are fixed (PERIOD-04) | Keep the books start and the year as they are |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Enter the opening balances](opening-balances.md)
- [Organisation settings](organisation-settings.md)
