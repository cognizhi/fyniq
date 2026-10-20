---
key: accounting.journals
type: task
title: Prepare, post and reverse journals
summary: Prepare a manual journal and save it as a draft, check what posting writes, post it, and reverse a posted journal with a reason
sources:
  - docs/user-guide/journals.md sha256:f3e44d7eb31d58fc7d425841796415d5657d2fc0ed028f09626a650200f0ec98
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - journal
  - manual journal
  - journal entry
  - accrual
  - post a journal
  - reverse a journal
  - void a journal
  - draft journal
---

# Prepare, post and reverse journals

## What journals are

- **Accounting → Journals** (Expert mode) lists the manual journals, newest first, each with its number, date, description, total (its debits), who prepared it, who posted it and its status: **Draft**, **Posted** or **Reversed**.
- A journal moves amounts between accounts (an accrual, a correction, a reclassification); its debits equal its credits, and it posts on its date.
- Narrow the list by status, or search part of a description or number.
- An Accountant or a Tax agent prepares journals; only an Accountant posts and reverses them; others with a finance role can read them.

(src: docs/user-guide/journals.md#what-it-is)

## Prepare a journal

- **New journal**: the **Date** and a **Description**, then each line's account, an optional description, and the amount under **Debit** or **Credit**, never both; **Add a line** for more.
- The balance under the lines says **Balanced** once debits equal credits in each currency; then **Save as a draft**.
- Changing a draft and selecting **Save the changes** keeps the earlier version in its history; Fyniq records who saved each version.
- Type amounts without a sign, as `1,234.56` or `S$1,234.56`; a credit goes under **Credit**. A line in another currency takes its currency code and the rate: how much of the base currency one unit buys.

(src: docs/user-guide/journals.md#prepare-a-journal)

## Check and post

- Open the draft: **Check** runs every posting rule and shows what posting writes in the base currency, saving nothing.
- **Post**: the journal takes its number and posts on its date; Fyniq records who posted it and when.
- A posted journal is never edited.

(src: docs/user-guide/journals.md#check-and-post-a-journal)

## Reverse a posted journal

- Open it, select **Reverse**, choose the date of the reversing entry (on or after the journal's date) and say why, then **Reverse**.
- A reversing entry with every line the other way round posts on that date; the journal stays in the books, marked **Reversed**, and the audit trail keeps the reason. A journal is reversed once.
- Related: opening the financial year so journals can post ([[accounting.periods]]); the accounts journals post to ([[accounting.chart]]); the lines they post, in the general ledger ([[reports.general-ledger]]).

(src: docs/user-guide/journals.md#reverse-a-posted-journal) (src: docs/user-guide/journals.md#related)

## Common problems

- STORAGE-PERIOD-01, no period covers the posting date: open the financial year in **Accounting → Periods and close**, then post again.
- STORAGE-INV-03, the period is closed: post in an open period, or ask for it to be reopened.
- LEDGER-09 or LEDGER-10, it does not balance: make the debits equal the credits in each currency.
- LEDGER-02 or LEDGER-03: at least two lines, each with an amount.
- LEDGER-05 or LEDGER-06: choose an active account that is not a group.
- LEDGER-07 or LEDGER-13: a rate only on a line in another currency.
- LEDGER-16: date a reversal on or after the journal's date.
- JOURNAL-02, not a draft: a posted journal is reversed, not changed.

(src: docs/user-guide/journals.md#common-problems)
