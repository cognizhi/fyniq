---
key: accounting.periods
title: Open the financial year and close the periods
mode: expert
roles: [accountant, controller, owner, auditor, tax_agent]
requirements: [FR-ORG-02, FR-GL-07, FR-GL-08, FM-4, AUD-04]
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

An Accountant or a Controller opens a year and runs the close checks; an Owner or a Controller
attests and closes a month. The Auditor and the Tax agent can see the periods. The page is in Expert mode.

## Open a financial year

1. Make sure **Settings → Organisation** has when the books start and when the first financial
   year ends.
2. Open **Accounting → Periods and close**.
3. Select **Open the year**, then **Open the year** again to confirm.

All the year's periods open at once. The next year can open once the year before it has started.

Once a period exists, when the books start, the month the year starts in and the end of the first
year no longer change.

## Close a month

Closing a month takes two people: one runs the close checks, and another founder attests them and
closes the month.

1. Open **Accounting → Periods and close** and select the month.
2. An Accountant or a Controller selects **Run the checks**. Each check says whether it is done,
   needs a look (⚠) or blocks the close (✖):
   - every earlier month is closed: months close in order;
   - the balances agree with the ledger;
   - the trial balance at the month's end adds up to zero;
   - draft journals dated in the month (⚠: once the month closes they cannot post on those dates);
   - in the month the books start, the opening balances are posted (⚠).
3. Another founder, an Owner or a Controller who did not run the checks, reads them, says what they
   attest and selects **Attest and close**. Fyniq asks them to confirm it's them first.

If anything dated in or before the month posts after the checks ran, the checks are out of date and
the close is refused: run them again. A closed month refuses every posting dated in it, by anyone.
The close keeps a fingerprint (a SHA-256 hash) of the trial balance at the month's end, and the
ledger revision it closed at.

Reopening a closed month needs a second founder's approval and a reason. It arrives soon.

## Common problems

| Message | What to do |
|---|---|
| The financial years are not set (PERIOD-01) | Enter when the books start and when the first year ends in **Settings → Organisation** |
| The year starts too far ahead (PERIOD-02) | Open it once the year before it has started |
| Not the next year to open (PERIOD-03) | Reload the page: someone else opened that year |
| The financial years are fixed (PERIOD-04) | Keep the books start and the year as they are |
| The checks are stale (CLOSE-02) | Run the checks again, then attest |
| An earlier period is open (CLOSE-03) | Close the earlier months first |
| The checks refused the close (CLOSE-04) | Fix what the checks found and run them again |
| Another founder attests the close (CLOSE-05) | Ask another founder: you ran the checks |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Enter the opening balances](opening-balances.md)
- [Organisation settings](organisation-settings.md)
