---
key: planning.cash
title: Cash ahead
mode: basic
roles: [owner, controller, accountant, approver, auditor, tax_agent, board_viewer]
requirements: [FR-PLN-09, FR-PLN-05, FR-PLN-08, FR-NTF-01]
status: draft
---

# Cash ahead

## What it is

**Planning › Cash ahead** shows the company's bank balance day by day from today, as the books expect it to move. It
starts from today's balances in the money accounts, a foreign one valued at today's rate, and adds:

- each open invoice on the day it is expected, at what is still owed on it;
- each open bill on the day it is expected to be paid;
- each approved payment batch not yet released, on its payment date. Its bills are not counted again;
- the next invoices and bills of each active recurring invoice and bill, at what their content comes to today.

Open it from **Planning** in the sidebar (Expert mode), from **Cash ahead** on Home, or with search.

## Choose the days

Select **30 days**, **60 days** (the default), **90 days** or **180 days**, or **13 weeks** for the same figures a week
at a time.

## Read the projection

- **Lowest balance** is the lowest balance at the end of a day in the range, and the first day it happens.
- **Balance in N days** is the balance at the end of the last day.
- The chart draws the balance each day (or week) and the buffer line. **Table** shows its figures; **PNG** and **CSV**
  download them.
- **What moves it** lists each receipt and payment by day, with why it is on that day:
  - **pays N days late on average**: the customer's average lateness over the invoices it paid in the last 12 months
    (an early payment counts as on time). For a bill, the same is read from how the company has paid that supplier;
  - **Due**: the due date, when there is no such history;
  - **Planned payment date**: a bill's planned date, when there is no history;
  - **Overdue since**: it was expected before today, so it is shown today.

## The buffer

The buffer is the cash to keep in hand. It is the months of net burn set in **Settings › Organisation** (6 unless
changed; see [Organisation settings](organisation-settings.md#cash-buffer)), times the average monthly net burn of the
last three closed months.

- **Never below** says the balance stays above it throughout the range.
- **Below from** a date, and a warning at the top, say when it first falls below.
- With no closed month yet, or when the company takes in more cash than it uses, there is no line, and the tile says
  why.

When the 60-day projection first falls below the buffer, those who can change the plans get a notification. They get
another when it is back above, and again if it falls a second time.

## What is not included yet

Pay runs and CPF, GST returns and corporate income tax join the projection with their own modules. Until then they are
listed under **Not included yet**, and the real lowest point may be lower than shown. A money account in a currency
with no rate for today is listed there too.

## Try a what-if

In Expert mode, each line of **What moves it** has **Move to** (delay a bill, chase an invoice) and **Leave out**. The
projection, the lowest balance and the buffer are worked out again with the change. **Reset** drops every change.

## Nothing here posts

A projection and its what-ifs never change the books, a document, a planned payment date or a batch: the page says
**Sandbox: nothing here posts**. To change when a bill is paid, plan it on the bill (see
[Bills](bills.md)).

## Common problems

- **A recurring invoice or bill is under Not included yet**: its content cannot be worked out today, for example its
  currency has no rate for today. The line says why; fix the recurring document, or enter the rate.
- **The balance looks too high.** Check **Not included yet**: payroll, CPF, GST and tax are not in it yet.

## Related

- [The Home page and finding your way around](home.md)
- [Bills](bills.md)
- [Organisation settings](organisation-settings.md)
- [Feasibility studies](feasibility.md)
