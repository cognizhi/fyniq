---
key: accounting.chart
type: task
title: Set up and keep the chart of accounts
summary: Load the standard Singapore start-up chart, add an account, rename, regroup or deactivate one, and what system accounts are
sources:
  - docs/user-guide/chart-of-accounts.md sha256:5c466248d66f66ef508e8118af03f96564f06c74e335f2d315ab5533f003ff2e
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - chart of accounts
  - CoA
  - add an account
  - new account
  - account code
  - deactivate account
  - standard chart
  - system account
---

# Set up and keep the chart of accounts

## What the chart is

- **Accounting → Chart of accounts** (Expert mode) lists every account the books post to, by type: assets, liabilities, equity, revenue and expenses.
- A group holds accounts under it; postings go to the accounts, never to a group.
- Each account shows its report group, function, default GST code and flags such as **System**, **Money** and **Control**.
- An Accountant or a Tax agent adds and changes accounts; others with a finance role can see them.

(src: docs/user-guide/chart-of-accounts.md#what-it-is)

## Start from the standard chart

- A new company starts with no accounts: **Load the standard chart**, and confirm.
- The standard Singapore start-up chart has 86 accounts with their report groups, cash-flow lines, functions, GST codes, flags and guidance; it loads only into a chart with no accounts, and can be renamed, regrouped and added to afterwards.

(src: docs/user-guide/chart-of-accounts.md#start-from-the-standard-chart)

## Add or change an account

- **New account**: a **Code** and **Name** and its **Type** (the code and type never change), the group it sits **Under**, and what applies (report group, cash-flow line, function, default GST code, a money account's currency); then **Add the account**.
- The pencil next to an account changes its name, group, report group, function, GST code and guidance; **Save the account**.
- Clearing **Active** stops new postings; its history stays. The control and payroll-cost flags change only with an approval.
- **System** accounts, such as accounts receivable, accounts payable and GST control, keep their code, type and role, and stay active.
- Related: organisation settings ([[settings.organisation]]); contacts, items and their revenue accounts ([[contacts.manage]]).

(src: docs/user-guide/chart-of-accounts.md#add-an-account) (src: docs/user-guide/chart-of-accounts.md#change-an-account) (src: docs/user-guide/chart-of-accounts.md#system-accounts) (src: docs/user-guide/chart-of-accounts.md#related)

## Common problems

- COA-01, not an account code: use 1 to 20 letters, digits, dots or hyphens, starting with a letter or digit.
- COA-05, not the form of a GST code: use the code's capital letters, such as TX.
- COA-08, the control or payroll-cost flag changes only with approval: leave it for now.
- COA-09, a system account stays active: keep it, and post new entries to another account.

(src: docs/user-guide/chart-of-accounts.md#common-problems)
