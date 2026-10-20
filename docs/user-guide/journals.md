---
key: accounting.journals
title: Prepare, post and reverse journals
mode: expert
roles: [accountant, tax_agent, controller, owner, auditor]
requirements: [FR-GL-01, FR-GL-12, FR-INT-14, ENG-24]
status: draft            # written with the journal screens (P1, p1-general-ledger-close); reviewed with the accountant
---

# Prepare, post and reverse journals

## What it is

**Accounting → Journals** lists the company's manual journals, newest first. A journal moves
amounts between accounts: an accrual, a correction, a reclassification. Its debits equal its
credits, and it posts on its date.

- Each journal shows its number, date, description, total (its debits), who prepared it, who posted
  it and its status: **Draft**, **Posted** or **Reversed**.
- Choose a status to narrow the list, or type part of a description or number in the search box.
- An Accountant or a Tax agent prepares journals. Only an Accountant posts and reverses them.
  Everyone else with a finance role can read them. The pages are in Expert mode.

## Prepare a journal

1. Select **New journal**.
2. Enter the **Date** the journal belongs to and a **Description** of what it is for.
3. For each line, choose the **account**, add a description if it helps, and enter the amount under
   **Debit** or **Credit**, never both. Select **Add a line** for more.
4. Watch the balance under the lines: it says **Balanced** once the debits equal the credits in each
   currency.
5. Select **Save as a draft**.

A draft keeps every version: when you change it and select **Save the changes**, the earlier version
stays in its history. Fyniq records who saved each version.

Type amounts without a sign, as `1,234.56` or `S$1,234.56`: a credit goes under **Credit**. For a
line in another currency, enter its currency code and the rate: how much of the base currency one
unit buys.

## Check and post a journal

1. Open the draft from the list.
2. Select **Check**. Fyniq runs every posting rule and shows what posting writes in the base
   currency, without saving anything.
3. Select **Post**. The journal takes its number and posts on its date.

Fyniq records who posted it and when. Once posted, a journal is never edited.

## Reverse a posted journal

1. Open the posted journal.
2. Select **Reverse**.
3. Choose the date of the reversing entry, on or after the journal's date, and say why.
4. Select **Reverse**.

A reversing entry with every line the other way round posts on that date. The journal stays in the
books, marked **Reversed**, and the audit trail keeps your reason. A journal is reversed once.

## Common problems

| Message | What to do |
|---|---|
| No period covers the posting date (STORAGE-PERIOD-01) | Open the financial year in **Accounting → Periods and close**, then post again |
| The period is closed (STORAGE-INV-03) | Post in an open period, or ask for the period to be reopened |
| The journal does not balance (LEDGER-09, LEDGER-10) | Make the debits equal the credits, in each currency |
| Fewer than two lines, or a line of zero (LEDGER-02, LEDGER-03) | Give the journal at least two lines, each with an amount |
| The account is a group or inactive (LEDGER-05, LEDGER-06) | Choose an active account that is not a group |
| A rate is missing, or entered on a base-currency line (LEDGER-07, LEDGER-13) | Enter a rate only on a line in another currency |
| A reversal dated before the journal (LEDGER-16) | Date the reversal on or after the journal's date |
| The journal is not a draft (JOURNAL-02) | A posted journal is reversed, not changed |

## Related

- [Open the financial year and close the periods](periods-and-close.md)
- [Set up and keep the chart of accounts](chart-of-accounts.md)
