---
key: accounting.opening-balances
title: Enter the opening balances
mode: expert
roles: [accountant, tax_agent, controller, owner, auditor]
requirements: [FR-GL-06, INV-13]
status: draft            # written with the opening balances screen (P1, p1-general-ledger-close); reviewed with the accountant
---

# Enter the opening balances

## What they are

**Accounting → Opening balances** holds each account's balance on the day the books start, so the
books carry on from where they were: the bank balances, what customers owe, what the company owes,
the share capital and the retained earnings. Each balance is a debit or a credit, and together they
add up to zero.

- They are dated on the day the books start, which **Settings → Organisation** sets.
- Every balance is in the base currency. Open invoices and bills in other currencies come with sales
  and purchases.
- An Accountant or a Tax agent enters them. Only an Accountant posts them. The page is in Expert
  mode.

## Enter the opening balances

1. Open **Accounting → Opening balances**. The balance-sheet accounts are listed; select **Show
   revenue and expense accounts** if the books start part-way through a year.
2. Enter each account's balance under **Debit** or **Credit**, never both. Leave accounts without a
   balance empty.
3. Say where the balances come from, such as the trial balance at the end of the last year.
4. Watch the total: it says **Balanced** once the debits equal the credits.
5. Select **Save as a draft**. You can come back and change the draft; each save keeps the one before
   it.

Type balances without a sign, as `1,234.56` or `S$1,234.56`: a credit balance goes under **Credit**.
If the balances do not add up, the difference usually belongs in retained earnings.

## Check and post them

1. Select **Check**. Fyniq runs the posting rules without saving anything.
2. Select **Post**. The balances post once, on the day the books start, as entered.

Share capital takes its opening balance here: after that, it changes only through the register of
share issues.

Posted opening balances do not change on this page yet. Changing them needs a second founder's
approval, which arrives soon; until then, correct them with a journal.

## Common problems

| Message | What to do |
|---|---|
| The books start is not set (JOURNAL-06) | Enter when the books start in **Settings → Organisation** |
| No period covers the posting date (STORAGE-PERIOD-01) | Open the first financial year in **Accounting → Periods and close** |
| The balances do not net to zero (LEDGER-09, LEDGER-10) | Make the debits equal the credits |
| A balance not in the base currency (LEDGER-17) | Enter it in the base currency |
| They are not a draft (JOURNAL-02) | Posted opening balances do not change here; correct them with a journal |

## Related

- [Open the financial year and close the periods](periods-and-close.md)
- [Prepare, post and reverse journals](journals.md)
