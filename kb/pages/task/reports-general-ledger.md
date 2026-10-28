---
key: reports.general-ledger
type: task
title: Read the general ledger
summary: Every posted line of chosen accounts between two dates, with opening, running and closing balances and each line's source, and exporting it as CSV
sources:
  - docs/user-guide/general-ledger.md sha256:d3ddbef5969d08bb48a780106783222be4af9dd5d88d9b8a375021fe657cf596
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - general ledger
  - GL
  - ledger report
  - account transactions
  - running balance
  - account history
  - retained earnings
  - year end
  - new financial year
---

# Read the general ledger

## What it is

- **Accounting → General ledger** (Expert mode) shows every posted line of the accounts chosen between two dates, in the base currency: the balance before the first day, each line in posting order with the balance after it, and the balance on the last day.
- Each line names its source, such as a journal's number, which opens the journal or the opening balances it came from; a line in another currency also shows its amount in that currency.
- It reads the ledger at one ledger revision: the same revision always gives the same report.
- It needs a role that views every report, such as the Accountant's or the Controller's; a view of only sales, purchases, payroll, a department or the board pack cannot read it.

(src: docs/user-guide/general-ledger.md#what-it-is)

## Read it

- Choose an account and select **Add**, up to 20 accounts; set **From** and **To** (by default the first day of this month to today).
- Each account shows its opening balance, its lines, and its closing balance with the debits and credits.
- At most 5,000 lines of one account show at once; for more, choose a shorter range.
- Related: the journals behind the lines ([[accounting.journals]]); the periods they post in ([[accounting.periods]]); the statements whose figures open it ([[reports.statements]]).

(src: docs/user-guide/general-ledger.md#read-the-general-ledger) (src: docs/user-guide/general-ledger.md#related)

## A new financial year

- Once the financial years are set in **Settings → Organisation** ([[settings.organisation]]), each year starts afresh for income and expenses: on a financial year's first day a revenue or expense account opens at nothing, and **Retained earnings** opens with the profit or loss of every earlier year.
- Bank, customer, supplier and other balance-sheet accounts carry on.
- Fyniq works this out whenever it reads a balance, so there is no closing entry to post.
- Read one financial year at a time: a range that starts in one year and ends in the next is refused.

(src: docs/user-guide/general-ledger.md#a-new-financial-year)

## Export it

- **Export CSV** gives one row per line, with each account's opening and closing balance.
- Amounts are plain decimals with an ASCII minus sign; text a spreadsheet would read as a formula starts with an apostrophe.

(src: docs/user-guide/general-ledger.md#export-it)

## Common problems

- REPORT-01, too many lines for one report: choose a shorter range of dates.
- REPORT-02, the range crosses a financial year: choose a range within one financial year, or read each year on its own.
- API-AUTHZ-01, your roles hold no right to it: ask an Owner for a role that views every report.

(src: docs/user-guide/general-ledger.md#common-problems)
