---
key: accounting.periods
title: Open the financial year and close the periods
mode: expert
roles: [accountant, controller, owner, auditor, tax_agent]
requirements: [FR-ORG-02, FR-GL-04, FR-GL-07, FR-GL-08, FR-GL-10, FR-GL-14, FM-4, AUD-04, INV-07]
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
   - automatic reversals that have not posted: dated on or before the month's end, or of a journal
     dated in the month and waiting for a month that is not open (⚠);
   - balances in other currencies on accounts revalued at month end, not revalued for the month
     since the last posting dated in it or before (✖); see
     [Exchange rates](currencies.md#revalue-a-month);
   - recurring journals due in the month with no draft yet (⚠), or a draft more than a day late,
     which means the scheduler failed (✖);
   - active recurring journals whose lines would not post (⚠);
   - accounts payable agrees with the bills still open and the credits with suppliers at the month's
     end (✖ when they differ by anything Fyniq cannot explain: report it, never adjust it by
     journal); amounts on accounts payable from opening balances or manual journals, which no bill
     supports, show ⚠: support them with the suppliers' statements, or enter the bills;
   - accounts receivable agrees with the invoices still open at the month's end, in the same way (✖
     when they differ by anything Fyniq cannot explain); amounts on accounts receivable from opening
     balances or manual journals, which no invoice supports, show ⚠: support them with the
     customers' statements, or enter the invoices;
   - income and expense accounts that moved against the previous month by more than both
     thresholds in **Settings → Organisation** (S$1,000 and 10% unless changed), with no
     explanation of their totals as they are now (✖; see [Explain the variances](#explain-the-variances));
   - the month's close checklist: every task done, or skipped for the month with a reason (✖; see
     [Work through the checklist](#work-through-the-checklist));
   - in the month the books start, the opening balances are posted (⚠).
3. Another founder, an Owner or a Controller who did not run the checks, reads them, says what they
   attest and selects **Attest and close**. Fyniq asks them to confirm it's them first.

If anything dated in or before the month posts after the checks ran, the checks are out of date and
the close is refused: run them again. A closed month refuses every posting dated in it, by anyone.
The close keeps a fingerprint (a SHA-256 hash) of the trial balance at the month's end, and the
ledger revision it closed at.

## Work through the checklist

Each month's close page starts with its **Checklist**: the tasks every month's close needs, such as
reconciling the bank accounts or reviewing accruals, each with who usually does it and when it is
due.

1. An Accountant or a Controller selects **Done** beside a task once it is done.
2. A task that does not apply this month, such as payroll before there are employees, is skipped
   with **Skip** and a reason. The founder who attests the close reads it.
3. Run the checks again: they refuse the close while a task is neither done nor skipped.

- A task marked by mistake is opened again with the undo button beside it.
- Fyniq keeps every mark: who marked the task, when, and the note or reason.
- Until the company changes it, the checklist is Fyniq's recommended one: the month's documents
  entered, the bank, card and wallet accounts reconciled, payroll and CPF posted, accruals and
  prepayments reviewed, foreign-currency balances revalued, and the month reviewed.

## Change the checklist template

1. On a month's close page, select **Change the template** under the checklist.
2. Change, add, remove or reorder the tasks. For each, say who usually does it and how many days
   after the month's end it is due.
3. Select **Save the template**.

- An Accountant or a Controller changes it. Every change is kept as a new version, with who saved
  it.
- Open months use the new template at once. A task the template keeps keeps its marks; a task it
  drops no longer counts.

## Explain the variances

Before a month closes, Fyniq compares each income and expense account's total for the month with
the previous month's. A change needs a short explanation when it is more than both thresholds in
**Settings → Organisation**: S$1,000 and 10% of the previous month's total, unless changed. When the
previous month's total is zero, the amount alone decides.

1. On the month's close page, **Variances to explain** lists the accounts that need one, with both
   months' totals and the change.
2. An Accountant or a Controller selects **Explain** beside an account, says why it moved, and
   selects **Explain** again.
3. Run the checks again: they see the explanations when they run.

- An explanation is of the two totals shown. If either changes afterwards, the account shows **The
  totals changed**, and the close asks for a new explanation.
- An explanation is never changed or deleted: a new one replaces the earlier one.
- **Show every income and expense account** also lists the accounts that moved within the
  thresholds.
- Totals are shown as in the trial balance, so income is negative.
- The first month of the books has no earlier month, so nothing is compared.

## Year end

Nothing is posted at year end, and there is no closing entry to make. Whenever Fyniq reads a
balance in a new financial year, income and expense accounts start the year from nothing, and the
earlier years' profit or loss is in the account with the retained-earnings role (3100 Retained
earnings in the standard chart). Balance-sheet accounts carry on as they are.

Each month's close page shows **Year end** for the financial year that holds the month:

- how many of the year's months are closed. The year is closed once its last month closes, and
  months close in order;
- the year's income, expenses and profit or loss, so far until the year is closed;
- retained earnings at the start of the year, what was posted to it in the year (such as the
  opening balances), the year's profit or loss, and what goes into the next year.

If no account has the retained-earnings role, the page says so, and income and expense accounts
carry on from year to year until one does.

The general ledger reads one financial year at a time, so a range of dates that crosses the start
of a year is refused.

## Reopen a month

Only the latest closed month reopens: months reopen in the reverse of the order they closed.

1. An Accountant or a Controller opens the closed month and selects **Ask to reopen**.
2. They say why, and confirm it's them.
3. Another founder who may close periods approves the request in their **Inbox**. Until then, the
   month stays closed.

Once approved, the month takes postings again until it is closed again. Fyniq keeps the close's
fingerprint as the one before, and takes a new one of the trial balance as the one after.

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
| The period is not closed (CLOSE-07) | Only a closed month reopens |
| A later period is closed (CLOSE-08) | Reopen the latest closed month first |
| The account has no variance to explain (CLOSE-09) | Explain one of the accounts under **Variances to explain** |
| The totals changed since they were read (CLOSE-10) | Reload the page and explain the totals as they are now |
| The checklist template is not valid (CLOSE-11) | Give each task a title, and a due day from 0 to 60 |
| No such task on the checklist (CLOSE-12) | Reload the page: the template changed |
| Skipping a task needs a reason (CLOSE-13) | Say why the task does not apply this month |
| A change to this record is waiting for approval already (APR-06) | Approve or reject the waiting request first |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Enter the opening balances](opening-balances.md)
- [Organisation settings](organisation-settings.md)
