---
key: purchases.orders
type: task
title: Purchase orders
summary: Raising a purchase order with quantities and prices, approving it, making bills of it line by line, and closing it
sources:
  - docs/user-guide/purchase-orders.md sha256:488e717d746de8d87c2885dc396df437ecaa9f0eaf0e98f6716691f3b0d35bfd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-23
aliases:
  - purchase order
  - PO
  - raise an order
  - approve an order
  - convert an order to a bill
  - bill an order
  - close an order
  - order quantity
  - part billed
---

# Purchase orders

## What it is

- A purchase order agrees what the company buys from a supplier, how many and at what price, before the bill arrives; they are under **Purchases → Bills → Orders**.
- An order never goes into the books by itself: the bills made from it do, each under its own approvals.
- Its states: **Draft**, **Awaiting approval** or **Rejected** until approved; then **Approved**, **Part billed** or **Billed** as bills take its lines; and **Closed** once closed.

(src: docs/user-guide/purchase-orders.md#what-it-is)

## Raise an order

- **New order**: choose the **Supplier** and the **Order date**, optionally a **Reference** (such as their quotation number) and when the goods or services are **Expected on**; then each line's **What it is for** (an expense or asset account), **Quantity**, **Unit price before GST** and **GST code**; **Save as a draft**.
- A line's amount is its quantity times its unit price, rounded to the currency's smallest unit (half up); the order shows the GST expected and its total.

(src: docs/user-guide/purchase-orders.md#raise-an-order)

## Approve an order

- **Approve** a saved draft: at or below the self-approval threshold (S$2,000 unless changed) you approve it alone, and in Founder mode it is listed for the weekly review; above it, **Ask for approval**, and another person who approves bills and did not prepare it approves it in the Inbox.
- An approved order takes its number (PO-0001 onwards) and never changes: close it and raise another to change it.

(src: docs/user-guide/purchase-orders.md#approve-an-order)

## Bill an order

- When the supplier's invoice arrives, **Make a bill** on the approved order and say how much of each line it covers; a draft bill opens for the supplier's **Invoice date** and **Their invoice number**, with amounts to correct to the invoice, and it posts as any bill ([[purchases.bills]]).
- A line billed in parts adds up exactly to its amount; a bill never takes more of a line than is left, and the order shows what each line ordered, what posted bills took and what is left.

(src: docs/user-guide/purchase-orders.md#bill-an-order)

## Close an order

- **Close order** with a reason: no more bills are made from it, a draft bill made from it no longer posts, and the bills posted already stand.

(src: docs/user-guide/purchase-orders.md#close-an-order)

## Common problems

- PO-03, the supplier is not an active vendor: choose a contact marked as a vendor, or reactivate it.
- PO-07, a quantity or price an order does not take: enter a quantity above zero and a price of zero or more.
- PO-14, another person's approval is needed: select **Ask for approval**; PO-15, you prepared it: ask another person who approves bills.
- PO-12 or PO-13, more than is left: bill at most what is left of each line; PO-11, not approved or closed: bill an approved order that is not closed.

(src: docs/user-guide/purchase-orders.md#common-problems)

## Related

- [[purchases.bills]], [[approvals.decide]]

(src: docs/user-guide/purchase-orders.md#related)
