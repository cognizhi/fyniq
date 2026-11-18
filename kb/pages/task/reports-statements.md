---
key: reports.statements
type: task
title: Read the financial statements
summary: The P&L, balance sheet, cash flow and trial balance for any months, compared with the past, budget or forecast, traced to their lines
sources:
  - docs/user-guide/financial-statements.md sha256:8883067d5b1e62b251fd784468ea420c1931ad6fb5b522ae91f21b30acb258cc
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-12
aliases:
  - trace a figure
  - drill down
  - where does this number come from
  - financial statements
  - profit and loss
  - P&L
  - department P&L
  - project P&L
  - income statement
  - balance sheet
  - cash flow
  - trial balance
  - EBITDA
  - management statements
  - reports
  - P&L against budget
  - budget column
  - forecast column
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

## Compare with the budget or the forecast

- On the profit and loss, **Compare with** also offers **The budget** and **The forecast**.
- **The budget**: a **Budget** column, account by account, from each month's financial-year budget once approved; its figures are the budget, not the books, so they open no ledger lines. A department's or project's profit and loss compares with the budget lines for it. If a month of the period is in a year with no approved budget, the column is left out and a note says why. The gap and whether it is favourable are in **Budget vs Actual** ([[planning.budgets]]).
- **The forecast**: a **Forecast** column from the latest recorded forecast, its first scenario with its funding. A forecast plans by drivers, not accounts, so the column has a figure only for revenue, cost of sales, gross profit, operating expenses, profit before tax and net profit. A forecast starts after the last month of actual figures, so the column appears only for months it covers, such as the months still to come this financial year.

(src: docs/user-guide/financial-statements.md#compare-with-the-budget-or-the-forecast)

## What each statement shows

- **Profit and loss:** revenue by stream, cost of sales, gross profit, operating expenses, EBITDA, depreciation, operating profit, other income and finance costs, tax and net profit; in Expert mode each line's percentage of revenue.
- **Balance sheet:** current and non-current assets, current liabilities and borrowings, and equity with the year's profit so far.
- **Cash flow:** the business's own activities, investing in equipment, and financing from shareholders and lenders, ending at the cash in the bank accounts; a transaction that moved no money, such as a cost a director paid personally, is listed apart.
- **Trial balance:** every account's balance, debits and credits, adding up to zero.
- Titled **Management statements** until the accountant confirms the year's framework and policies in **Settings → Accounting** ([[settings.accounting]]); the layout comes from each account's report group, function and cash-flow line in the chart ([[accounting.chart]]) and waits for the accountant's confirmation too.

(src: docs/user-guide/financial-statements.md#what-each-statement-shows)

## By department or project

- When the organisation uses dimensions, such as Department or Project, the profit and loss shows them in Expert mode ([[accounting.chart]]).
- **Columns by dimension** gives a column for each of its values, such as ENG Engineering, one for the lines with no value of it, and their total, which the others add up to; it shows one run of months, without a comparison.
- **Only for** shows the profit and loss of one value alone, such as the Engineering department.
- The balance sheet, the cash flow and the trial balance show the whole company: a department's lines are one side of their entries, so its own figures would not balance.

(src: docs/user-guide/financial-statements.md#by-department-or-project)

## Trace a figure to its source

- An account's figure, or a group's or a section's total, opens the **Trace** panel beside the statement, in four steps.
- **Breakdown**: the accounts a total is made of, each with its figure; an account's own figure starts at its lines.
- **Ledger lines**: the account's lines for the same months, and of the column's value or the value chosen, with the balance before and after them, read at the statement's ledger revision: the lines the figure was read from, even if something was posted since.
- **Source**: the entry a line belongs to and its document (number, customer or supplier, date, status now, a link to open it); Expert mode adds the entry's lines.
- **History**: what happened to that document, from the audit trail, only for people whose roles read the audit trail; others are told so.
- The path at the top goes back a step; **Back** or closing the panel returns to the statement as it was; **Open as a full page** shows the account's lines in the general ledger report.
- A computed figure such as gross profit does not open, nor does a figure of the lines with no value of a dimension, nor a budget's.

(src: docs/user-guide/financial-statements.md#trace-a-figure-to-its-source)

## Export a statement

- **Export** opens a dialog: **Excel (XLSX)**, a workbook with the statement and a **Data** tab of every account's figure in every column; **PDF**, the statement to print or send, with page numbers and the ledger revision on every page (landscape past five columns); or **CSV**, its rows as plain numbers.
- The export is the statement as on screen (period, comparison, columns), read at the same ledger revision as the screen's; amounts are numbers, not text, and nothing in the file becomes a formula. A comparison with the forecast is on screen only for now: the export leaves it out.
- Each export is recorded in the audit trail, with what was exported and its filters. A PDF that cannot be made just then says so: try again in a moment, or export to Excel or CSV.
- The dialog also lists your recent accountant exports, to download one that is ready or ask again for one that expired.

(src: docs/user-guide/financial-statements.md#export-a-statement)

## Accountant exports

- In Expert mode **Reports** has the **Accountant exports**: the general ledger and the journal entries over a range of days, and the trial balance, the fixed asset schedule, and the receivables and payables in detail at a day, as Excel or CSV.
- Choose the report, its days and the format, then **Prepare the export**; it is prepared in the background, the list says when it is ready and a notification tells you; then **Download**.
- Only the person who asked can download it, for 24 hours; then it expires and its file is deleted; **Ask again** beside an expired export makes a fresh one.
- The general ledger export has no limit of lines: it is written as it is read. An Excel sheet holds at most 1,048,576 rows, so a longer one goes on in a second sheet named after the first with (2), its header repeated.
- An export that cannot be made says why, such as a general ledger range that crosses the start of a financial year (export each year apart). Each export and download is recorded in the audit trail.

(src: docs/user-guide/financial-statements.md#accountant-exports)

## The same figures again

- Every statement is read at one ledger revision, shown at the bottom; asked again at it, it gives the same figures whatever was posted since.
- An account renamed or regrouped after it shows **Restated presentation**: the same figures, today's names.

(src: docs/user-guide/financial-statements.md#the-same-figures-again)

## Common problems

- The balance sheet does not balance: an account with a balance sits in no section, and the note names it.
- Accounts in no section: no cash-flow line, one the statements do not know, or one of the other side; the cash flow still shows their movements under **Other accounts**. Give each a cash-flow line for its type in Accounting → Chart of accounts.
- A red note that the cash flow does not tie: the books' own checks should prevent it; report it to the accountant.
- No months for the choice: open the financial year in Accounting → Periods and close, or choose other months.

(src: docs/user-guide/financial-statements.md#common-problems)
