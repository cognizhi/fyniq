---
key: accounting.periods
type: task
title: Open the financial year and close the periods
summary: What accounting periods are, opening the next financial year, closing a month with two people, explaining its variances, and year end
sources:
  - docs/user-guide/periods-and-close.md sha256:049cecc3508933a720d343c5d36f97e0f1ee8a5229ac71c2f2e8efd05e4219bf
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-28
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
  - reopen a month
  - variance
  - flux analysis
  - close checklist
  - year end
  - retained earnings
  - closing entry
  - month-end revaluation
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
- The checks: every earlier month closed (months close in order); the balances agreeing with the ledger; the trial balance at the month's end adding up to zero; draft journals dated in the month (⚠); automatic reversals that have not posted, dated on or before the month's end or of a journal dated in the month and waiting for a month that is not open (⚠); balances in other currencies on accounts revalued at month end, not revalued for the month since the last posting dated in it or before (✖; [[accounting.currencies]]); recurring journals due in the month with no draft yet (⚠), or a draft more than a day late, which means the scheduler failed (✖); active recurring journals whose lines would not post (⚠) ([[accounting.journals]]); accounts payable agreeing with the bills still open and the credits with suppliers at the month's end (✖ when they differ by anything Fyniq cannot explain: report it, never adjust it by journal), with amounts from opening balances or manual journals that no bill supports shown ⚠, to support with the suppliers' statements or by entering the bills ([[purchases.bills]]); accounts receivable agreeing with the invoices still open and the credits with customers at the month's end in the same way (✖ when they differ by anything Fyniq cannot explain), with amounts from opening balances or manual journals that no invoice supports shown ⚠, to support with the customers' statements or by entering the invoices; customer deposits agreeing with the deposits still held at the month's end in the same way (✖ when they differ by anything Fyniq cannot explain), with amounts from opening balances or manual journals that no deposit supports shown ⚠, to support with the customers' statements or by recording the deposits; prepayments and deferred revenue agreeing with the costs and income still spread over the months ahead at the month's end in the same way (✖ when they differ by anything Fyniq cannot explain), with amounts from opening balances or manual journals shown ⚠, to release by journal as the months pass or have the accountant check, and a cost or income that a bill or a bank line put on them directly, which nothing releases, shown ⚠, to spread over its months instead or release by journal ([[accounting.schedules]]); accounts with a balance that sit in no section of the balance sheet because their cash-flow line is not one of the standard chart's (⚠: their figures are missing from the statements; give each a cash-flow line in the chart of accounts, [[accounting.chart]]); settlement differences cleared to FX and rounding in the month above the monthly cap in **Settings → Organisation** (⚠: the cap was lowered after they cleared; review them); income and expense accounts that moved against the previous month by more than both thresholds in **Settings → Organisation** (S$1,000 and 10% unless changed), with no explanation of their totals as they are now (✖; see Explain the variances below) ([[settings.organisation]]); the month's close checklist, every task done or skipped for the month with a reason (✖; see Work through the checklist below); in the month the books start, the opening balances posted (⚠). ✖ blocks the close, ⚠ needs a look.
- If anything dated in or before the month posts after the checks ran, they are out of date and the close is refused: run them again.
- A closed month refuses every posting dated in it, by anyone; the close keeps a SHA-256 fingerprint of the trial balance at the month's end and the ledger revision it closed at.
- Related: preparing and posting journals ([[accounting.journals]]); entering the opening balances ([[accounting.opening-balances]]).

(src: docs/user-guide/periods-and-close.md#close-a-month) (src: docs/user-guide/periods-and-close.md#related)

## Work through the checklist

- Each month's close page starts with its **Checklist**: the tasks every month's close needs, each with who usually does it and when it is due.
- An Accountant or a Controller selects **Done** once a task is done; a task that does not apply this month, such as payroll before there are employees, is skipped with **Skip** and a reason the attesting founder reads. Then run the checks again: they refuse the close while a task is neither done nor skipped.
- A task marked by mistake is opened again with the undo button; Fyniq keeps every mark with who, when and the note or reason.
- Until the company changes it, the checklist is Fyniq's recommended one: the month's documents entered, the bank, card and wallet accounts reconciled, payroll and CPF posted, accruals and prepayments reviewed, foreign-currency balances revalued, and the month reviewed.

(src: docs/user-guide/periods-and-close.md#work-through-the-checklist)

## Change the checklist template

- On a month's close page, **Change the template** under the checklist; change, add, remove or reorder tasks, each with who usually does it and how many days after the month's end it is due; then **Save the template**.
- An Accountant or a Controller changes it; every change is kept as a new version with who saved it.
- Open months use the new template at once: a task it keeps keeps its marks, and a task it drops no longer counts.

(src: docs/user-guide/periods-and-close.md#change-the-checklist-template)

## Explain the variances

- Before a month closes, each income and expense account's total for the month is compared with the previous month's; a change needs a short explanation when it is more than both thresholds in **Settings → Organisation**: S$1,000 and 10% of the previous month's total, unless changed ([[settings.organisation]]). When the previous month's total is zero, the amount alone decides.
- On the month's close page, **Variances to explain** lists the accounts that need one with both months' totals and the change; an Accountant or a Controller selects **Explain** beside an account, says why it moved and selects **Explain** again, then runs the checks again so they see it.
- An explanation is of the two totals shown: if either changes, the account shows **The totals changed** and the close asks for a new one. Explanations are never changed or deleted; a new one replaces the earlier one.
- **Show every income and expense account** also lists those that moved within the thresholds. Totals are shown as in the trial balance, so income is negative. The first month of the books has nothing to compare.

(src: docs/user-guide/periods-and-close.md#explain-the-variances)

## Year end

- Nothing is posted at year end and there is no closing entry to make: whenever a balance is read in a new financial year, income and expense accounts start the year from nothing, and the earlier years' profit or loss is in the account with the retained-earnings role (3100 Retained earnings in the standard chart, [[accounting.chart]]); balance-sheet accounts carry on.
- Each month's close page shows **Year end** for the financial year that holds the month: how many of its months are closed (the year is closed once its last month closes, and months close in order); its income, expenses and profit or loss, so far until it is closed; and retained earnings at the start of the year, posted to it in the year (such as the opening balances, [[accounting.opening-balances]]), the year's profit or loss, and what goes into the next year.
- Without an account with the retained-earnings role, the page says so, and income and expense accounts carry on from year to year until one has it.
- The general ledger reads one financial year at a time, so a range across the start of a year is refused ([[reports.general-ledger]]).

(src: docs/user-guide/periods-and-close.md#year-end)

## Reopen a month

- Only the latest closed month reopens: months reopen in the reverse of the order they closed.
- An Accountant or a Controller opens the closed month, selects **Ask to reopen**, says why and confirms it's them; another founder who may close periods approves it in their **Inbox**, and until then the month stays closed.
- Once approved, the month takes postings again until it is closed again; Fyniq keeps the close's fingerprint as the one before and takes a new one as the one after.

(src: docs/user-guide/periods-and-close.md#reopen-a-month)

## Common problems

- PERIOD-01, the financial years are not set: enter the books start and the first year end in **Settings → Organisation**.
- PERIOD-02, the year starts too far ahead: open it once the year before it has started.
- PERIOD-03, not the next year to open: reload the page, as someone else opened that year.
- PERIOD-04, the financial years are fixed: keep the books start and the year as they are.
- CLOSE-02, the checks are stale: run them again, then attest.
- CLOSE-03, an earlier period is open: close the earlier months first.
- CLOSE-04, the checks refused the close: fix what they found and run them again.
- CLOSE-05, another founder attests the close: ask another founder, as you ran the checks.
- CLOSE-07, the period is not closed: only a closed month reopens.
- CLOSE-08, a later period is closed: reopen the latest closed month first.
- CLOSE-09, the account has no variance to explain: explain one of the accounts under **Variances to explain**.
- CLOSE-10, the totals changed since they were read: reload the page and explain the totals as they are now.
- CLOSE-11, the checklist template is not valid: give each task a title, and a due day from 0 to 60.
- CLOSE-12, no such task on the checklist: reload the page, as the template changed.
- CLOSE-13, skipping a task needs a reason: say why it does not apply this month.
- APR-06, a request is waiting already: approve or reject it first.

(src: docs/user-guide/periods-and-close.md#common-problems)
