---
key: accounting.schedules
type: task
title: See what is spread over months
summary: The schedules of costs paid ahead and income received ahead, each month posted, waiting or still ahead, and how Prepayments and Deferred revenue tie to them
sources:
  - docs/user-guide/schedules.md sha256:900a7b7edf7d6ba7fe36f237b1a4ec5408f885129487e5d396a6428ab169cd99
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-10-28
aliases:
  - schedule
  - schedules
  - prepayment
  - prepaid cost
  - deferred revenue
  - income received in advance
  - spread over months
  - amortise a prepayment
---

# See what is spread over months

## What it is

- **Accounting → Schedules** (Expert mode) lists every cost and income spread over the months it belongs to, such as a year's software subscription paid in advance, insurance, or a customer's annual fee received up front.
- A cost spread over months sits on **1210 Prepayments** and moves to its expense account a month at a time; income sits on **2400 Deferred revenue** and moves to its revenue account the same way.
- A schedule comes from a bill line or an invoice line spread over months, chosen where the line is entered ([[purchases.bills]], [[sales.invoices]]), or from a bank line's part spread through the API.
- Each month posts only once it is reached, on its first day (or on the document's date, for months before it); later months are shown but nothing is in the books for them yet.
- Anyone who can read the full reports sees it: the Owner, the Administrator, the Controller, the Accountant, the Bookkeeper, the Auditor and the Tax agent.

(src: docs/user-guide/schedules.md#what-it-is)

## Read a schedule

- Each schedule shows its line, the account each month posts to, its total, its months (such as "Sept 2026–Dec 2026 · 2 of 4": two of four posted), what it has recognised and what remains; select one to see its months.
- **Posted**: in the books. **Waiting**: reached, not posted yet by the hourly run, or its month is not open; it posts once the month is open ([[accounting.periods]]). **Future**: not reached. **Reversed**: posted, then reversed because its document was changed or voided.
- The months are rounded on the running total, so they can differ by a cent and always add up to the total exactly.
- **As at** shows another day, such as a month's end; **Show finished schedules** adds those with nothing left.

(src: docs/user-guide/schedules.md#read-a-schedule)

## Check Prepayments and Deferred revenue

- The two tiles tie each account to its schedules, as the month's close does.
- **Ties to the ledger**: the account holds what its schedules still hold, and nothing Fyniq cannot explain.
- An amount **entered on the account directly** (a bill or a bank line on 1210 itself, not spread) is never expensed by itself: spread the line instead, or release it by journal as the months pass ([[accounting.journals]]).
- Amounts from **opening balances or manual journals** are named: release them by journal as the months pass, or have the accountant check them.
- **Differs by** an amount: a posting and its document disagree, and the close refuses the month until it is resolved; report it, never adjust it by journal.

(src: docs/user-guide/schedules.md#check-prepayments-and-deferred-revenue)

## The roll-forward

- The **Roll-forward** tab shows how Prepayments (by supplier) and Deferred revenue (by customer) moved over a run of months, chosen as YYYY-MM: **Opening**, **Added**, **Released**, **Reversed** and **Closing**.
- Closing is always Opening plus Added less Released plus Reversed; a reversed release, a credit note, an edit or a cancellation is Reversed and can move the balance either way. The total ties to the account in the ledger, and a difference is named and refused by the close.
- Each customer also shows, in the base currency, what it was **Billed** (its invoices and credit notes dated inside the run, GST included, a credit note negative), what it **Collected** (its receipts), what was **Recognised** from it (released from its schedules plus what its invoices credited straight to revenue) and what is **Still deferred**.
- The check beside each customer compares what it owes at the end of the run with the day before: billed less collected, plus its other documents (a refund, a credit applied, a write-off or a deposit), must explain the change. An amount no document explains is shown, never hidden: ask the Accountant to look at that customer.

(src: docs/user-guide/schedules.md#the-roll-forward)

## Spread a line

- On a bill, give a line on an expense account **Spread over** a number of months (1 to 120) and the month its service starts; a bill that spreads a line cannot also put another line on 1210 itself.
- On an invoice, do the same on a line on a revenue account.

(src: docs/user-guide/schedules.md#spread-a-line)
