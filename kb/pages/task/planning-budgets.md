---
key: planning.budgets
type: task
title: Budgets and Budget vs Actual
summary: A year's budget by account and month in versions, copied, imported, approved and locked, and Budget vs Actual
sources:
  - docs/user-guide/budgets.md sha256:e77eb289762777c4fc6611904ed38b8e0422c84df344df205bd10ec98aa88a73
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-08
aliases:
  - budget
  - make a budget
  - budget vs actual
  - variance against budget
  - import a budget from excel
  - approve a budget
  - revised budget
---

# Budgets and Budget vs Actual

## What a budget is

- **Planning › Budgets** (Expert mode) holds each financial year's budget: an amount for each month by income or expense account and, where needed, by dimension value. A budget comes in versions: a draft is changed freely; another person approves it, which records and locks it; the year's budget is its latest recorded version. A budget never posts.
- The year's budget is read by **Reports › Budget vs Actual**, the profit and loss's **Compare with › The budget** ([[reports.statements]]), the close's **Variances against the budget** ([[accounting.periods]]) and Home's revenue and costs against budget ([[home.overview]]).

(src: docs/user-guide/budgets.md#what-it-is)

## Start, copy or import one

- **New budget**: a name and a financial year; then **Add a line** per account, or account and dimension value, with each month's amount, signed as the profit and loss shows them (income and costs both above zero); **Save** works the totals out. Only income and expense accounts that take postings, with at most one value of each dimension.
- **Copy with growth**: the version shown into a new draft, each amount × (1 + growth) rounded once to the cent (0.05 for 5% more, −0.1 for 10% less). **Original** for another year with as many months; **Revised** for a new version of the same year, revising its recorded budget.
- **Import XLSX**: the first sheet of an .xlsx workbook, a header row of **Account**, a column per dimension by its name and a column per month (such as 2027-01), then a row per line with the account's code, each dimension's value code and the amounts. Nothing is drafted until every row is right; each problem says its row, column and how to fix it.

(src: docs/user-guide/budgets.md#start-a-budget) (src: docs/user-guide/budgets.md#copy-with-growth) (src: docs/user-guide/budgets.md#import-from-a-spreadsheet)

## Approve and lock

- **Ask for approval** on a draft (confirming it's you if not recently); another person who approves budgets (an Owner or the Controller) decides it in their Inbox, never the one who asked.
- Approved, the version is **Recorded, locked**: it never changes and is the year's budget; a later version approved for the same year supersedes it.

(src: docs/user-guide/budgets.md#approve-and-lock)

## Budget vs Actual

- **Reports › Budget vs Actual** compares each income and expense account's actual figures with the year's budget for months of one financial year (the year to date unless others are chosen): the actual, the budget, the variance (actual less budget), its percentage of the budget, and **Favourable** or **Unfavourable** (income at or above its budget, costs at or below theirs), with income, cost and net totals.
- **By dimension** shows each account and set of dimension values apart. With no recorded budget for the year, every budget figure is 0 and the page says so. Related: [[planning.cash]].

(src: docs/user-guide/budgets.md#budget-vs-actual) (src: docs/user-guide/budgets.md#related)
