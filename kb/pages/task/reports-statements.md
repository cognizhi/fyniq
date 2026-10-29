---
key: reports.statements
type: task
title: Read the financial statements
summary: The profit and loss, balance sheet, cash flow and trial balance for any run of months, comparisons, tracing a figure to its lines, and re-running at a revision
sources:
  - docs/user-guide/financial-statements.md sha256:d237993822feda62064239a45563ffd2c9cb5f13915e03936f3999a8a425cdf9
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - financial statements
  - profit and loss
  - P&L
  - income statement
  - balance sheet
  - cash flow
  - trial balance
  - EBITDA
  - management statements
  - reports
---

# Read the financial statements

## Find a report

- **Reports** opens with the essentials, the profit and loss, the balance sheet and the cash flow, each with this month's headline figure; below them the other reports, such as who owes the company and whom it owes, and in Expert mode the trial balance, the general ledger ([[reports.general-ledger]]), the schedules ([[accounting.schedules]]) and the management reports: sales by customer and item, spend by supplier and the top expenses ([[reports.management]]).
- **Find a report** narrows the list.
- Anyone who can read every account sees the statements: the Owner, the Administrator, the Controller, the Accountant, the Bookkeeper, the Auditor, the Tax agent and the DPO.

(src: docs/user-guide/financial-statements.md#find-a-report)

## Choose the period

- A tab per statement: **Profit and loss**, **Balance sheet**, **Cash flow** and, in Expert mode, **Trial balance**.
- **Period**: this month, last month, the financial year to date, this financial year, last financial year, or any run of months. The balance sheet and the trial balance are at the end of the last month; the balance sheet also shows how equity moved over the months chosen.
- In Expert mode **Compare with** adds the period before or the same months a year earlier, and **A column per month** shows each month apart; on the profit and loss a chart then draws revenue, gross profit and net profit a line each, month by month, with **Table**, **PNG** and **CSV**.
- While the last month is not closed, a note says its figures can still change ([[accounting.periods]]).

(src: docs/user-guide/financial-statements.md#choose-the-period)

## What each statement shows

- **Profit and loss:** revenue by stream, cost of sales, gross profit, operating expenses, EBITDA, depreciation, operating profit, other income and finance costs, tax and net profit; in Expert mode each line's percentage of revenue.
- **Balance sheet:** current and non-current assets, current liabilities and borrowings, and equity with the year's profit so far.
- **Cash flow:** the business's own activities, investing in equipment, and financing from shareholders and lenders, ending at the cash in the bank accounts; a transaction that moved no money, such as a cost a director paid personally, is listed apart.
- **Trial balance:** every account's balance, debits and credits, adding up to zero.
- Titled **Management statements** until the accountant confirms the year's framework and policies in **Settings → Accounting** ([[settings.accounting]]); the layout comes from each account's report group, function and cash-flow line in the chart ([[accounting.chart]]) and waits for the accountant's confirmation too.

(src: docs/user-guide/financial-statements.md#what-each-statement-shows)

## See the lines behind a figure

- An account's figure, or a group's total, opens the general ledger at its lines for the same months; a computed figure such as gross profit does not open.

(src: docs/user-guide/financial-statements.md#see-the-lines-behind-a-figure)

## Export a statement

- **Export** opens a dialog: **Excel (XLSX)**, a workbook with the statement and a **Data** tab of every account's figure in every column; **PDF**, the statement to print or send, with page numbers and the ledger revision on every page (landscape past five columns); or **CSV**, its rows as plain numbers.
- The export is the statement as on screen (period, comparison, columns), read at the same ledger revision as the screen's; amounts are numbers, not text, and nothing in the file becomes a formula.
- Each export is recorded in the audit trail, with what was exported and its filters. A PDF that cannot be made just then says so: try again in a moment, or export to Excel or CSV.
- The dialog also lists your recent accountant exports, to download one that is ready or ask again for one that expired.

(src: docs/user-guide/financial-statements.md#export-a-statement)

## Accountant exports

- In Expert mode **Reports** has the **Accountant exports**: the general ledger and the journal entries over a range of days, and the trial balance, the fixed asset schedule, and the receivables and payables in detail at a day, as Excel or CSV.
- Choose the report, its days and the format, then **Prepare the export**; it is prepared in the background, the list says when it is ready and a notification tells you; then **Download**.
- Only the person who asked can download it, for 24 hours; then it expires and its file is deleted; **Ask again** beside an expired export makes a fresh one.
- An export that cannot be made says why, such as a general ledger range that crosses the start of a financial year (export each year apart). Each export and download is recorded in the audit trail.

(src: docs/user-guide/financial-statements.md#accountant-exports)

## The same figures again

- Every statement is read at one ledger revision, shown at the bottom; asked again at it, it gives the same figures whatever was posted since.
- An account renamed or regrouped after it shows **Restated presentation**: the same figures, today's names.

(src: docs/user-guide/financial-statements.md#the-same-figures-again)

## Common problems

- A red note that the statement does not balance or tie: the books' own checks should prevent it; report it to the accountant.
- Accounts in no section: give the account one of the standard chart's cash-flow lines in Accounting → Chart of accounts.
- No months for the choice: open the financial year in Accounting → Periods and close, or choose other months.

(src: docs/user-guide/financial-statements.md#common-problems)
