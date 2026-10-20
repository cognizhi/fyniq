---
key: accounting.periods
type: task
title: Open the financial year and close the periods
summary: What accounting periods are, how the financial years run from the books start, and how to open the next financial year so journals can post
sources:
  - docs/user-guide/periods-and-close.md sha256:d556da37a5cdcdd61ec277cd4c4dfad90c89183077c123a9a33814be43162f70
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - period
  - accounting period
  - financial year
  - open the year
  - books start
  - period close
  - month end
---

# Open the financial year and close the periods

## What periods are

- **Accounting → Periods and close** (Expert mode) shows the periods a financial year at a time; each period is a calendar month.
- A journal or document posts in the month of its date, and only while that month is **Open**; nothing posts on a date no open period covers.
- The first financial year runs from the day the books start to the end of the first year, so its first period is short when the books start mid-month; each later year is the twelve months after the one before.
- An Accountant or a Controller opens a year; the Owner, the Auditor and the Tax agent can see the periods.

(src: docs/user-guide/periods-and-close.md#what-it-is)

## Open a financial year

- First, **Settings → Organisation** needs when the books start and when the first financial year ends ([[settings.organisation]]).
- On **Accounting → Periods and close**, select **Open the year** and confirm: all the year's periods open at once.
- The next year can open once the year before it has started.
- Once a period exists, the books start, the month the year starts in and the end of the first year no longer change.

(src: docs/user-guide/periods-and-close.md#open-a-financial-year)

## Closing a period

- Closing a month, with its checks and a second founder's attestation, joins this page soon; a closed period refuses every posting dated in it.
- Related: preparing and posting journals ([[accounting.journals]]); entering the opening balances ([[accounting.opening-balances]]).

(src: docs/user-guide/periods-and-close.md#closing-a-period) (src: docs/user-guide/periods-and-close.md#related)

## Common problems

- PERIOD-01, the financial years are not set: enter the books start and the first year end in **Settings → Organisation**.
- PERIOD-02, the year starts too far ahead: open it once the year before it has started.
- PERIOD-03, not the next year to open: reload the page, as someone else opened that year.
- PERIOD-04, the financial years are fixed: keep the books start and the year as they are.

(src: docs/user-guide/periods-and-close.md#common-problems)
