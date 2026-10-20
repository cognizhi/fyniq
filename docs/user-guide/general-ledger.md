---
key: reports.general-ledger
title: Read the general ledger
mode: expert
roles: [accountant, controller, owner, bookkeeper, auditor, tax_agent]
requirements: [FR-GL-11]
status: draft            # written with the general ledger screen (P1, p1-general-ledger-close); reviewed with the accountant
---

# Read the general ledger

## What it is

The **general ledger** shows every posted line of the accounts you choose between two dates, in the
base currency: each account's balance before the first day, each line in the order it posted with
the balance after it, and the balance on the last day. Open it from **Accounting → General
ledger**.

- Each line names its source, such as a journal's number: select it to open the journal or the
  opening balances it came from.
- A line in another currency also shows its amount in that currency.
- The report reads the ledger at one moment, its ledger revision: the same revision always gives
  the same report.
- It needs a role that views every report, such as the Accountant's or the Controller's. A role
  that views only sales, purchases, payroll, a department or the board pack cannot read it. The page
  is in Expert mode.

## Read the general ledger

1. Choose an account and select **Add**. Add more accounts the same way, up to 20.
2. Set **From** and **To**. The report starts at the first day of this month and ends today.
3. Read each account's opening balance, its lines and its closing balance with the debits and the
   credits.

At most 5,000 lines of one account show at once. If an account has more, choose a shorter range.

## A new financial year

Once the company's financial years are set in **Settings → Organisation**, each year starts afresh
for income and expenses: on the first day of a financial year, a revenue or expense account opens at
nothing, and **Retained earnings** opens with the profit or loss of every earlier year. Bank,
customer, supplier and other balance-sheet accounts carry on. Fyniq works this out whenever it reads
a balance, so there is no closing entry to post.

Read one financial year at a time: a range that starts in one year and ends in the next is refused.

## Export it

Select **Export CSV**. The file has one row per line, with each account's opening and closing
balance. Amounts are plain decimals with an ASCII minus sign, ready for a spreadsheet, and text a
spreadsheet would read as a formula starts with an apostrophe.

## Common problems

| Message | What to do |
|---|---|
| Too many lines for one report (REPORT-01) | Choose a shorter range of dates |
| The range crosses a financial year (REPORT-02) | Choose a range within one financial year, or read each year on its own |
| Your roles hold no right to it (API-AUTHZ-01) | Ask an Owner for a role that views every report |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Open the financial year and close the periods](periods-and-close.md)
