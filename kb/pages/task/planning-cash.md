---
key: planning.cash
type: task
title: Cash ahead, the short-term cash flow
summary: The bank balance day by day for 30 to 180 days or 13 weeks, its lowest point, the buffer line, what is not included yet, and what-ifs
sources:
  - docs/user-guide/cash-flow.md sha256:ac5124ccb7e4b9f1f1fc95e67b962d8efecda15bd33712701a9e6ca4912e2810
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-08
aliases:
  - cash flow forecast
  - short-term cash flow
  - 13-week cash flow
  - when do we run out of money
  - lowest balance
  - cash buffer
  - what if a customer pays late
  - delay a bill
---

# Cash ahead, the short-term cash flow

## What it shows

- **Planning › Cash ahead** shows the bank balance day by day from today, as the books expect it to move. It starts from today's balances in the money accounts (a foreign one valued at today's rate).
- It adds each open invoice on the day it is expected, at what is still owed; each open bill on the day it is expected to be paid; each approved payment batch not yet released on its payment date, its bills not counted again; and the next invoices and bills of each active recurring invoice and bill, at what their content comes to today.
- It opens from **Planning** in the sidebar (Expert mode), from **Cash ahead** on Home ([[home.overview]]), or with search.

(src: docs/user-guide/cash-flow.md#what-it-is)

## Choose the days and read it

- **30 days**, **60 days** (the default), **90 days**, **180 days**, or **13 weeks** for the same figures a week at a time.
- **Lowest balance**: the lowest balance at the end of a day in the range and the first day it happens; **Balance in N days**: the balance at the end of the last day.
- The chart draws the balance and the buffer line, with **Table**, **PNG** and **CSV**.
- **What moves it** lists each receipt and payment by day and why it is on that day: **pays N days late on average** (the customer's average lateness over invoices paid in the last 12 months, an early payment counting as on time; for a bill, how the company has paid that supplier); **Due** (no history); **Planned payment date** (a bill's plan, no history); **Overdue since** (expected before today, so shown today).

(src: docs/user-guide/cash-flow.md#choose-the-days) (src: docs/user-guide/cash-flow.md#read-the-projection)

## The buffer and its alert

- The buffer is the months of net burn set in **Settings › Organisation** (6 unless changed; [[settings.organisation]]) times the average monthly net burn of the last three closed months.
- **Never below** says the balance stays above it; **Below from** a date, and a warning, say when it first falls below. With no closed month yet, or when the company takes in more cash than it uses, there is no line and the tile says why.
- When the 60-day projection first falls below the buffer, those who can change the plans are notified; again when it is back above, and again if it falls a second time.

(src: docs/user-guide/cash-flow.md#the-buffer)

## Not included yet

- Pay runs and CPF, GST returns and corporate income tax join with their own modules; until then they are listed under **Not included yet**, and the real lowest point may be lower than shown. A money account in a currency with no rate for today is listed there too, and so is a recurring invoice or bill whose content cannot be worked out today, with why.

(src: docs/user-guide/cash-flow.md#what-is-not-included-yet) (src: docs/user-guide/cash-flow.md#common-problems)

## What-ifs, and nothing posts

- In Expert mode each line of **What moves it** has **Move to** (delay a bill, chase an invoice) and **Leave out**; the projection, the lowest balance and the buffer are worked out again with the change, and **Reset** drops every change.
- A projection and its what-ifs never change the books, a document, a planned payment date or a batch: the page says **Sandbox: nothing here posts**. To change when a bill is paid, plan it on the bill ([[purchases.bills]]).

(src: docs/user-guide/cash-flow.md#try-a-what-if) (src: docs/user-guide/cash-flow.md#nothing-here-posts)
