---
key: reports.statements
title: Read the financial statements
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, auditor, tax_agent, dpo]
requirements: [FR-RPT-01, FR-RPT-02, FR-RPT-08, INV-10, INV-11]
status: draft            # written with the statements (P1); the layout waits for the accountant's confirmation
---

# Read the financial statements

## Find a report

**Reports** opens with the essentials: the profit and loss, the balance sheet and the cash flow,
each with this month's headline figure. Below them are the other reports, such as who owes the
company and whom it owes; in Expert mode also the trial balance, the general ledger, the
schedules and the management reports: sales by customer and item, spend by supplier and the top
expenses. Type in **Find a report** to narrow the list.

Anyone who can read every account sees the statements: the Owner, the Administrator, the
Controller, the Accountant, the Bookkeeper, the Auditor, the Tax agent and the DPO.

## Choose the period

Each statement has a tab: **Profit and loss**, **Balance sheet**, **Cash flow** and, in Expert
mode, **Trial balance**.

- **Period** chooses the months: this month, last month, the financial year to date, this
  financial year, last financial year, or any run of months.
- The balance sheet and the trial balance are at the end of the last month. The balance sheet also
  shows how equity moved over the months chosen.
- In Expert mode, **Compare with** adds the period before or the same months a year earlier, and
  **A column per month** shows each month apart.

While the last month is not closed, a note says its figures can still change.

## What each statement shows

- **Profit and loss:** revenue by stream, the cost of sales, gross profit, operating expenses,
  EBITDA, depreciation, the operating profit, other income and finance costs, tax and the net
  profit. In Expert mode each line shows its percentage of revenue.
- **Balance sheet:** what the company owns (current and non-current assets) and owes (current
  liabilities and borrowings), and equity, which includes the year's profit so far.
- **Cash flow:** where the cash came from and went: the business's own activities, investing in
  equipment, and financing from shareholders and lenders. It ends at the cash in the bank
  accounts. A transaction that moved no money, such as a cost a director paid personally, is listed
  apart below it.
- **Trial balance:** every account's balance, debits and credits, which add up to zero.

The statements are titled **Management statements** until the accountant confirms the financial
year's framework and policies in **Settings → Accounting**. The way accounts are laid out comes from
each account's report group, function and cash-flow line in the chart of accounts, and waits for
the accountant's confirmation too.

## See the lines behind a figure

Select an account's figure, or a group's total, to open the general ledger at its lines for the
same months. A computed figure, such as gross profit, does not open.

## Export a statement

Select **Export** and choose the format:

- **Excel (XLSX)**: a workbook with the statement and a **Data** tab of every account's figure in
  every column.
- **PDF**: the statement to print or send, with the organisation's name, the statements' title,
  page numbers and the ledger revision on every page. It turns to landscape when it has more than
  five columns, such as a column per month.
- **CSV**: the statement's rows, as plain numbers.

The export is the statement as on screen: its period, comparison and columns, with its figures
read at the same ledger revision as the screen's. Amounts are numbers, not text, and nothing in the
file becomes a formula. Each export is recorded in the audit trail, with what was exported and its
filters. If the PDF cannot be made just then, the dialog says so: try again in a moment, or export
to Excel or CSV.

The dialog also lists your recent accountant exports (below), so you can download one that is
ready or ask again for one that expired.

## Accountant exports

In Expert mode, **Reports** also has the **Accountant exports**: the general ledger and the journal
entries over a range of days, and the trial balance, the fixed asset schedule, and the receivables
and payables in detail at a day, as Excel or CSV.

1. Choose the report, its days and the format, and select **Prepare the export**.
2. It is prepared in the background; the list below says when it is ready, and a notification
   tells you.
3. Select **Download**. Only the person who asked for an export can download it, for 24 hours;
   then it expires and its file is deleted. Select **Ask again** beside an expired export for a
   fresh one.

An export that cannot be made says why, such as a general ledger range that crosses the start of a
financial year: export each year apart. Each export and each download is recorded in the audit
trail.

## The same figures again

Every statement is read at one ledger revision, shown at the bottom. Asked again at that revision,
it gives the same figures, whatever was posted since. If an account was renamed or regrouped after
it, the statement says **Restated presentation**: the figures are the same, and the names are
today's.

## Common problems

- **A red note says the statement does not balance or tie.** The books' own checks should prevent
  it: report it to the accountant.
- **Accounts sit in no section.** An account has a cash-flow line the statements do not know. Give
  it one of the standard chart's cash-flow lines in Accounting → Chart of accounts.
- **The books have no months for this choice.** Open the financial year in Accounting → Periods and
  close, or choose other months.
