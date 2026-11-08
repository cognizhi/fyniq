---
key: planning.budgets
title: Budgets and Budget vs Actual
mode: expert
roles: [owner, controller, accountant, approver, auditor, tax_agent, board_viewer]
requirements: [FR-PLN-01, FR-RPT-03, FR-RPT-02, FR-GL-14, FR-KPI-01]
status: draft
---

# Budgets and Budget vs Actual

## What it is

**Planning › Budgets** (Expert mode) holds each financial year's budget: an amount for each month, by income or expense
account and, where you need it, by dimension value, such as a department. A budget comes in versions. A draft is
changed freely; another person approves it, which records and locks it, and the year's budget is its latest recorded
version. A budget never posts.

The year's budget is read by:

- **Reports › Budget vs Actual** (see [Budget vs Actual](#budget-vs-actual));
- the profit and loss's **Compare with › The budget** (see [Read the financial statements](financial-statements.md));
- the close's **Variances against the budget** (see [Close a month](periods-and-close.md#against-the-budget));
- Home's revenue and costs against budget (see [The Home page](home.md#read-the-plans-on-home)).

## Start a budget

1. Select **New budget**, name it and choose its financial year.
2. Select **Add a line** for each account, or account and dimension value, and enter each month's amount. Amounts are
   signed as the profit and loss shows them: income and costs both above zero for what you expect.
3. Select **Save**. The totals by month and for the year are worked out when you save.

Only income and expense accounts that take postings can be budgeted, with at most one value of each dimension.

## Copy with growth

**Copy with growth** copies the version shown into a new draft, each amount × (1 + growth), rounded once to the cent:
0.05 for 5% more, −0.1 for 10% less.

- **Original** copies it to another year with as many months, such as next year's budget from this year's.
- **Revised** makes a new version of the same year, revising its recorded budget.

## Import from a spreadsheet

**Import XLSX** reads the first sheet of an .xlsx workbook into a new draft: a header row of **Account**, a column for
each dimension by its name, and a column for each month of the year (such as 2027-01), then a row for each line with the
account's code, each dimension's value code and the month's amounts. Nothing is drafted until every row is right:
each problem says its row, its column and how to fix it.

## Approve and lock

1. On a draft, select **Ask for approval**. You confirm it's you if you have not recently.
2. Another person who approves budgets (an Owner or the Controller) decides it in their Inbox. You never approve your
   own.
3. Approved, the version is **Recorded, locked**: it never changes, and it is the year's budget. A later version
   approved for the same year supersedes it.

## Budget vs Actual

**Reports › Budget vs Actual** compares each income and expense account's actual figures with the year's budget, for
months of one financial year (the year to date unless you choose others). It shows:

- the actual, the budget, the variance (actual less budget) and the variance as a percentage of the budget;
- **Favourable** or **Unfavourable**: income at or above its budget, or costs at or below theirs, is favourable;
- the income, cost and net totals.

**By dimension** shows each account and set of dimension values apart, such as advertising for Sales. With no recorded
budget for the year, every budget figure is 0, and the page says so.

## Related

- [Read the financial statements](financial-statements.md)
- [Close a month](periods-and-close.md)
- [Cash ahead](cash-flow.md)
