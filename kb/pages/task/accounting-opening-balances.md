---
key: accounting.opening-balances
type: task
title: Enter the opening balances
summary: Enter each account's balance at the books start as a debit or a credit, save it as a draft, check it nets to zero, and post it once
sources:
  - docs/user-guide/opening-balances.md sha256:a2eba789042022dc650555e2b12bbfc197512d137c6752e513cbbac15008edda
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - opening balance
  - opening balances
  - balances at books start
  - brought forward
  - starting balances
  - trial balance at start
---

# Enter the opening balances

## What they are

- **Accounting → Opening balances** (Expert mode) holds each account's balance on the day the books start, so the books carry on from where they were: bank balances, what customers owe, what the company owes, share capital and retained earnings.
- Each balance is a debit or a credit, and together they add up to zero; they are dated on the day the books start, set in **Settings → Organisation**.
- Every balance is in the base currency; open invoices and bills in other currencies come with sales and purchases.
- An Accountant or a Tax agent enters them; only an Accountant posts them.

(src: docs/user-guide/opening-balances.md#what-they-are)

## Enter them

- The balance-sheet accounts are listed; **Show revenue and expense accounts** when the books start part-way through a year.
- Enter each balance under **Debit** or **Credit**, never both, and leave accounts without a balance empty; say where the balances come from.
- The total says **Balanced** once debits equal credits; then **Save as a draft**. Each save keeps the one before it.
- Type balances without a sign, as `1,234.56` or `S$1,234.56`; a difference usually belongs in retained earnings.

(src: docs/user-guide/opening-balances.md#enter-the-opening-balances)

## Check and post

- **Check** runs the posting rules without saving anything; **Post** posts the balances once, on the day the books start, as entered.
- Share capital takes its opening balance here; after that it changes only through the register of share issues.
- Posted opening balances do not change on the page yet: changing them needs a second founder's approval, which arrives soon; until then, correct them with a journal ([[accounting.journals]]).
- Related: opening the first financial year first ([[accounting.periods]]).

(src: docs/user-guide/opening-balances.md#check-and-post-them) (src: docs/user-guide/opening-balances.md#related)

## Common problems

- JOURNAL-06, the books start is not set: enter it in **Settings → Organisation**.
- STORAGE-PERIOD-01, no period covers the posting date: open the first financial year in **Accounting → Periods and close**.
- LEDGER-09 or LEDGER-10: make the debits equal the credits.
- LEDGER-17, a balance not in the base currency: enter it in the base currency.
- JOURNAL-02, not a draft: posted opening balances do not change here; correct them with a journal.

(src: docs/user-guide/opening-balances.md#common-problems)
