---
key: accounting.chart
title: Set up and keep the chart of accounts
mode: expert
roles: [accountant, tax_agent, controller, owner, auditor]
requirements: [FR-COA-01, FR-COA-02, FR-COA-03, FR-COA-04, FR-COA-05, FR-GL-04, FR-DIM-01]
status: draft            # written with the chart of accounts screen (P1); reviewed with the accountant's CoA review
---

# Set up and keep the chart of accounts

## What it is

**Accounting → Chart of accounts** lists every account the books post to, by type: assets,
liabilities, equity, revenue and expenses. A group holds accounts under it; postings go to the
accounts, never to a group. Each account shows its report group, its function, its default GST code
and flags such as **System**, **Money** and **Control**.

An Accountant or a Tax agent adds and changes accounts. Everyone else in the company with a finance
role can see them. The page is in Expert mode.

## Start from the standard chart

A new company starts with no accounts.

1. Open **Accounting → Chart of accounts**.
2. Select **Load the standard chart**, then **Load the standard chart** again to confirm.

The standard Singapore start-up chart has 88 accounts, with their report groups, cash-flow lines,
functions, GST codes, flags and guidance. It loads only into a chart with no accounts. You can
rename, regroup and add accounts afterwards.

## Add an account

1. Select **New account**.
2. Enter its **Code** and **Name**, and choose its **Type**. The code and type never change once it
   is added.
3. Choose the group it sits **Under**, and fill in what applies: report group, cash-flow line,
   function, default GST code, and for a bank, card or cash account its currency.
4. Select **Add the account**.

## Change an account

Select the pencil next to an account, change what you need, and select **Save the account**.

- You can rename it, move it under another group, make it a group, and change its report group,
  function, GST code and guidance.
- Clear **Active** to stop new postings to it. Its history stays.
- The control and payroll-cost flags change only with an approval, which arrives with the approval
  engine.
- Tick **Revalued at month end** on an asset or a liability settled in money, such as a bank
  account in another currency, receivables or a director's loan: its balances in other currencies
  are restated each month at the month-end rate. The standard chart ticks it on the money accounts,
  accounts receivable and payable, staff and director advances, the director's loan and deferred
  remuneration. Prepayments, deferred revenue and equity, income and expense accounts are never
  revalued.
- On an asset account of the fixed asset register (report group Property, plant & equipment or
  Intangible assets), set its **Default useful life (months)**: registering an asset on it offers
  that life. The standard chart sets 36 months for 1510 Computer equipment, 60 for 1520 Office
  equipment & furniture, 36 for 1530 Renovation & fit-out and 36 for 1540 Software & intangible
  assets; the company's accountant confirms them. Changing it never changes an asset already
  registered. Leave it empty for none.

## Merge an account

When two accounts hold the same kind of thing, such as SaaS tools and software subscriptions, merge
one into the other. Its balance moves into the other account by one entry, and it becomes inactive
for good. Every earlier posting stays where it is, so earlier months and closed months read as
before.

1. Select the merge button next to the account (it shows only on accounts a merge can take).
2. Choose the account to **Merge it into**: one of the same type, held in the same currency, that is
   not a group, a system, money or control account.
3. Choose the day its balance moves: a day in an open period, on or after its last posting.
4. Select **Ask for approval**. An Owner or a Controller who did not ask approves it; until then
   nothing moves, and the chart shows **Merge awaiting approval**.

On the approval, a balance-sheet account moves its whole balance; an income or expense account moves
its movement in the financial year, since earlier years are in retained earnings. Each currency and
dimension value moves at the amount the books recorded. The account then shows **Merged into** the
other account and takes no posting. A merge waits while something still posts to the account by
itself: a spread with months to release to it, a fixed asset on it, or a journal that reverses by
itself. If either account or its balance changes before the approval, the approval is void: ask
again.

## Dimensions

Dimensions tag amounts beyond the account, so you can see costs and revenue by team, by project or
by anything else you track. The **Dimensions** section under the accounts lists them, each with
its values in use.

- **Department** and **Project** are built in: add each once, if you use it.
- Add up to 8 dimensions of your own, such as Region or Funding source, with **Add a dimension**.
  With Department and Project that makes 10 at most.
- Select the pencil next to a dimension to rename it, or clear **In use** to put it out of use. A
  dimension is never deleted, and its kind never changes. Out of use, it keeps its values, and a
  custom one keeps its place among the 8.

An Accountant or a Tax agent adds and changes dimensions; everyone who can see the chart can see
them.

## Values of a dimension

Select **Values** next to a dimension to see its values, such as each department.

1. Under **Add a value**, enter a short **Code**, unique in the dimension, such as ENG, and a
   **Name**, such as Engineering.
2. Select **Add the value**.

A value's code never changes. Select the pencil next to a value to rename it, or clear **In use** to
put it out of use. A value is never deleted, and the lines that carry it keep it. A dimension out of
use takes no new values until it is back in use.

## Require a dimension

Select the pencil next to an account, tick each dimension every line on it needs under **Required
dimensions**, such as Department on the expense accounts, and select **Save the account**.

- A journal line you enter on it without a value of each is refused. Amounts Fyniq posts by itself,
  from an invoice or a bank line for example, still post, and the month's close flags them: move
  each to its value with a journal whose lines on the account net to zero (see
  [Close a month](periods-and-close.md#close-a-month)).
- Only a dimension in use can be added. A dimension out of use requires nothing while it is out of
  use.
- In the **Dimensions** list, **Required on** shows the accounts that require each dimension.
- Lines posted before keep what they have.

## System accounts

Accounts marked **System**, such as accounts receivable, accounts payable and GST control, have a
role the books rely on. Their code, type and role never change, and they stay active.

## Common problems

| Message | What to do |
|---|---|
| The code is not an account code (COA-01) | Use 1 to 20 letters, digits, dots or hyphens, starting with a letter or digit |
| Not the form of a GST code (COA-05) | Use the code's capital letters, such as TX |
| The control or payroll-cost flag changes only with approval (COA-08) | Leave the flag as it is for now |
| A system account stays active (COA-09) | Keep it active; use another account for new postings |
| The account is not revalued at month end (COA-10) | Tick it only on an asset or a liability settled in money |
| The organisation cannot add that dimension (DIM-01) | Department and Project come once each, and 8 of your own at most: rename one out of use and put it back in use instead |
| The name does not fit (DIM-02) | Give a name no other dimension, or no other value of the dimension, has, whatever the case |
| The value's code does not fit (DIM-03) | Use a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, not used by another value of the dimension |
| Not a dimension in use (DIM-04) | Put the dimension back in use before adding values to it |
| The accounts do not merge (MERGE-01) | Choose an active account of the same type and currency that is not a group, a system, money or control account |
| The merge's date does not fit (MERGE-02) | Choose a day in an open period, on or after the account's last posting |
| Something scheduled still posts to the account (MERGE-03) | Let the spread, asset or reversal post, or move it to another account, then merge |

## Related

- [Organisation settings](organisation-settings.md)
- [Keep your contacts, items and prices](contacts-and-items.md)
- [Exchange rates](currencies.md)
