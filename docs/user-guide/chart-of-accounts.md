---
key: accounting.chart
title: Set up and keep the chart of accounts
mode: expert
roles: [accountant, tax_agent, controller, owner, auditor]
requirements: [FR-COA-01, FR-COA-02, FR-COA-03, FR-COA-04, FR-COA-05]
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

The standard Singapore start-up chart has 86 accounts, with their report groups, cash-flow lines,
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

## Related

- [Organisation settings](organisation-settings.md)
- [Keep your contacts, items and prices](contacts-and-items.md)
