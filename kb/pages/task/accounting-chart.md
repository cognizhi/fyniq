---
key: accounting.chart
type: task
title: Set up and keep the chart of accounts
summary: Load the standard chart; add, change, deactivate or merge accounts; system accounts; dimensions, their values and the ones an account requires
sources:
  - docs/user-guide/chart-of-accounts.md sha256:dc9b789d6b8d49f6f95da772c61b64fdb77faf4eb5c4de09e0c4d64ecd6a39d4
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
aliases:
  - revalued at month end
  - monetary account
  - chart of accounts
  - CoA
  - add an account
  - new account
  - account code
  - deactivate account
  - merge accounts
  - merge an account
  - combine two accounts
  - standard chart
  - system account
  - dimension
  - dimensions
  - tracking category
  - department
  - project
  - cost centre
  - dimension value
  - required dimension
  - department required
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
- The standard Singapore start-up chart has 89 accounts with their report groups, cash-flow lines, functions, GST codes, flags and guidance; it loads only into a chart with no accounts, and can be renamed, regrouped and added to afterwards.

(src: docs/user-guide/chart-of-accounts.md#start-from-the-standard-chart)

## Add or change an account

- **New account**: a **Code** and **Name** and its **Type** (the code and type never change), the group it sits **Under**, and what applies (report group, cash-flow line, function, default GST code, a money account's currency); then **Add the account**.
- The pencil next to an account changes its name, group, report group, function, GST code and guidance; **Save the account**.
- Clearing **Active** stops new postings; its history stays. The control and payroll-cost flags change only with an approval.
- **Revalued at month end**, ticked on an asset or a liability settled in money (a bank account in another currency, receivables, a director's loan), restates its balances in other currencies each month at the month-end rate ([[accounting.currencies]]); the standard chart ticks the money accounts, accounts receivable and payable, staff and director advances, the director's loan and deferred remuneration; prepayments, deferred revenue and equity, income and expense accounts are never revalued.
- **Default useful life (months)**, on an asset account of the fixed asset register (report group Property, plant & equipment or Intangible assets), is the life offered when an asset on it is registered ([[accounting.fixed-assets]]): the standard chart sets 36 months for 1510 Computer equipment, 60 for 1520 Office equipment & furniture, 36 for 1530 Renovation & fit-out and 36 for 1540 Software & intangible assets, which the accountant confirms; changing it never changes an asset already registered.
- **System** accounts, such as accounts receivable, accounts payable and GST control, keep their code, type and role, and stay active.
- Related: organisation settings ([[settings.organisation]]); contacts, items and their revenue accounts ([[contacts.manage]]).

(src: docs/user-guide/chart-of-accounts.md#add-an-account) (src: docs/user-guide/chart-of-accounts.md#change-an-account) (src: docs/user-guide/chart-of-accounts.md#system-accounts) (src: docs/user-guide/chart-of-accounts.md#related)

## Merge an account

- Merging moves an account's balance into another account by one entry; the account becomes inactive for good, and every earlier posting stays where it is, so earlier and closed months read as before.
- The merge button next to an account, then the account to **Merge it into** (same type, same currency, not a group, a system, money or control account) and the day its balance moves (in an open period, on or after its last posting), then **Ask for approval** ([[approvals.decide]]).
- An Owner or a Controller who did not ask approves it; until then nothing moves and the chart shows **Merge awaiting approval**.
- On approval a balance-sheet account moves its whole balance and an income or expense account its movement in the financial year; each currency and dimension value moves at the amount the books recorded; the account then shows **Merged into** the other and takes no posting.
- A merge waits while something still posts to the account by itself (a spread, a fixed asset, a journal that reverses by itself); a change to either account or its balance before the approval voids it: ask again.

(src: docs/user-guide/chart-of-accounts.md#merge-an-account)

## Dimensions and their values

- Dimensions tag amounts beyond the account, to see costs and revenue by team, by project or by anything else tracked; the **Dimensions** section under the accounts lists each with its values in use.
- **Department** and **Project** are built in, once each; **Add a dimension** adds up to 8 of your own, such as Region or Funding source: 10 at most with Department and Project.
- The pencil renames a dimension, or clearing **In use** puts it out of use; a dimension is never deleted and its kind never changes; out of use it keeps its values, and a custom one keeps its place among the 8.
- **Values** next to a dimension lists its values: under **Add a value**, a short **Code** unique in the dimension (such as ENG) and a **Name** (such as Engineering), then **Add the value**.
- A value's code never changes; the pencil renames it or puts it out of use; a value is never deleted and the lines that carry it keep it; a dimension out of use takes no new values until it is back in use.
- An Accountant or a Tax agent adds and changes dimensions; everyone who can see the chart can see them.

(src: docs/user-guide/chart-of-accounts.md#dimensions) (src: docs/user-guide/chart-of-accounts.md#values-of-a-dimension)

## Require a dimension

- The pencil next to an account, then each dimension every line on it needs under **Required dimensions**, such as Department on the expense accounts, then **Save the account**.
- A journal line entered on it without a value of each is refused ([[accounting.journals]]); amounts Fyniq posts by itself, from an invoice or a bank line for example, still post and the month's close flags them: move each to its value with a journal whose lines on the account net to zero ([[accounting.periods]]).
- Only a dimension in use can be added; a dimension out of use requires nothing while it is out of use.
- In the **Dimensions** list, **Required on** shows the accounts that require each dimension; lines posted before keep what they have.

(src: docs/user-guide/chart-of-accounts.md#require-a-dimension)

## Common problems

- COA-01, not an account code: use 1 to 20 letters, digits, dots or hyphens, starting with a letter or digit.
- COA-05, not the form of a GST code: use the code's capital letters, such as TX.
- COA-08, the control or payroll-cost flag changes only with approval: leave it for now.
- COA-09, a system account stays active: keep it, and post new entries to another account.
- COA-10, the account is not revalued at month end: tick it only on an asset or a liability settled in money.
- DIM-01, the organisation cannot add that dimension: Department and Project come once each, and 8 of your own at most; rename one out of use and put it back in use instead.
- DIM-02, the name does not fit: give a name no other dimension, or no other value of the dimension, has, whatever the case.
- DIM-03, the value's code does not fit: a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, not used by another value of the dimension.
- DIM-04, not a dimension in use: put the dimension back in use before adding values to it.
- MERGE-01, the accounts do not merge: choose an active account of the same type and currency that is not a group, a system, money or control account.
- MERGE-02, the merge's date does not fit: a day in an open period, on or after the account's last posting.
- MERGE-03, something scheduled still posts to the account: let the spread, asset or reversal post, or move it to another account, then merge.

(src: docs/user-guide/chart-of-accounts.md#common-problems)
