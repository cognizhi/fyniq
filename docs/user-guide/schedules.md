---
key: accounting.schedules
title: See what is spread over months
mode: expert
roles: [owner, administrator, controller, accountant, bookkeeper, auditor, tax_agent]
requirements: [FR-GL-03, ENG-05, INV-07]
status: draft            # written with the schedules (P1); reviewed with the accountant's sign-off of the close
---

# See what is spread over months

## What it is

**Accounting → Schedules** lists every cost and income that is spread over the months it belongs
to: a year's software subscription paid in advance, insurance, or a customer's annual fee received
up front.

- A cost spread over months sits on **1210 Prepayments** and moves to its expense account one month
  at a time. Income spread over months sits on **2400 Deferred revenue** and moves to its revenue
  account the same way.
- A schedule comes from a bill line or an invoice line spread over months, which you choose where
  you enter the line, or from a bank line's part spread through the API. The schedule follows by
  itself.
- Each month posts only once that month is reached, on the month's first day (or on the document's
  date, for months before it). Later months are shown, but nothing is in the books for them yet.
- The page is in Expert mode. Anyone who can read the full reports can see it: the Owner, the
  Administrator, the Controller, the Accountant, the Bookkeeper, the Auditor and the Tax agent.

## Read a schedule

Each schedule shows the line it comes from, the account each month posts to, its total, its months
(such as "Sept 2026–Dec 2026 · 2 of 4": two of its four months posted), what it has recognised and
what remains. Select one to see its months:

- **Posted**: in the books.
- **Waiting**: the month has been reached, and the hourly run has not posted it yet, or its month is
  not open. It posts once the month is open.
- **Future**: the month has not been reached.
- **Reversed**: posted, then reversed because its document was changed or voided.

The months are rounded on the running total, so they can differ by a cent and always add up to the
total exactly.

Choose **As at** to see the schedules on another day, such as a month's end. **Show finished
schedules** adds the ones with nothing left.

## Check Prepayments and Deferred revenue

The two tiles at the top tie each account to its schedules, as the month's close does:

- **Ties to the ledger** when the account holds what its schedules still hold, and nothing else
  Fyniq cannot explain.
- An amount **entered on the account directly** (a bill or a bank line put on 1210 itself, with no
  spread) is never expensed by itself. Spread the line instead, or release it by journal as the
  months pass.
- Amounts from **opening balances or manual journals** are named. Release them by journal as the
  months pass, or have the accountant check them.
- **Differs by** an amount means a posting and its document disagree. The close refuses the month
  until it is resolved: report it, and never adjust it by journal.

See [Close a month](periods-and-close.md#close-a-month).

## Spread a line

- On a bill, give a line on an expense account **Spread over** a number of months (1 to 120) and the
  month its service starts. A bill that spreads a line cannot also put another line on 1210 itself.
- On an invoice, do the same on a line on a revenue account.
