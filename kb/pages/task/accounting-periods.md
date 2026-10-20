---
key: accounting.periods
type: task
title: Open the financial year and close the periods
summary: What accounting periods are, how the financial years run from the books start, opening the next financial year, and closing a month with two people
sources:
  - docs/user-guide/periods-and-close.md sha256:f98ffbb5a9642fa17f4769fba8e41570f0cafbdb728fa9e4b49f128980b4d678
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
  - close a month
  - attestation
---

# Open the financial year and close the periods

## What periods are

- **Accounting → Periods and close** (Expert mode) shows the periods a financial year at a time; each period is a calendar month.
- A journal or document posts in the month of its date, and only while that month is **Open**; nothing posts on a date no open period covers.
- The first financial year runs from the day the books start to the end of the first year, so its first period is short when the books start mid-month; each later year is the twelve months after the one before.
- An Accountant or a Controller opens a year and runs the close checks; an Owner or a Controller attests and closes a month; the Auditor and the Tax agent can see the periods.

(src: docs/user-guide/periods-and-close.md#what-it-is)

## Open a financial year

- First, **Settings → Organisation** needs when the books start and when the first financial year ends ([[settings.organisation]]).
- On **Accounting → Periods and close**, select **Open the year** and confirm: all the year's periods open at once.
- The next year can open once the year before it has started.
- Once a period exists, the books start, the month the year starts in and the end of the first year no longer change.

(src: docs/user-guide/periods-and-close.md#open-a-financial-year)

## Close a month

- Closing takes two people: an Accountant or a Controller selects **Run the checks** on the month; another founder, an Owner or a Controller who did not run them, says what they attest and selects **Attest and close**, confirming it's them first.
- The checks: every earlier month closed (months close in order); the balances agreeing with the ledger; the trial balance at the month's end adding up to zero; draft journals dated in the month (⚠); in the month the books start, the opening balances posted (⚠). ✖ blocks the close, ⚠ needs a look.
- If anything dated in or before the month posts after the checks ran, they are out of date and the close is refused: run them again.
- A closed month refuses every posting dated in it, by anyone; the close keeps a SHA-256 fingerprint of the trial balance at the month's end and the ledger revision it closed at.
- Reopening needs a second founder's approval and a reason; it arrives soon.
- Related: preparing and posting journals ([[accounting.journals]]); entering the opening balances ([[accounting.opening-balances]]).

(src: docs/user-guide/periods-and-close.md#close-a-month) (src: docs/user-guide/periods-and-close.md#related)

## Common problems

- PERIOD-01, the financial years are not set: enter the books start and the first year end in **Settings → Organisation**.
- PERIOD-02, the year starts too far ahead: open it once the year before it has started.
- PERIOD-03, not the next year to open: reload the page, as someone else opened that year.
- PERIOD-04, the financial years are fixed: keep the books start and the year as they are.
- CLOSE-02, the checks are stale: run them again, then attest.
- CLOSE-03, an earlier period is open: close the earlier months first.
- CLOSE-04, the checks refused the close: fix what they found and run them again.
- CLOSE-05, another founder attests the close: ask another founder, as you ran the checks.

(src: docs/user-guide/periods-and-close.md#common-problems)
