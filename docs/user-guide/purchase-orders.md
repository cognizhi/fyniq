---
key: purchases.orders
title: Purchase orders
mode: basic
roles: [bookkeeper, ap_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AP-01, RND-LINE, FM-2, FM-3, SOD-3]
status: draft            # written with the purchase order screens (P1, p1-purchases); reviewed with the accountant
---

# Purchase orders

## What it is

A purchase order agrees what the company buys from a supplier, how many and at what price, before
the bill arrives. Find them under **Purchases → Bills → Orders**. An order never goes into the books
by itself: the bills made from it do, each under its own approvals.

An order shows **Draft**, **Awaiting approval** or **Rejected** until it is approved; then
**Approved**, **Part billed** or **Billed** as bills take its lines; and **Closed** once someone
closes it.

## Raise an order

1. Select **New order**.
2. Choose the **Supplier**, the **Order date** and, if you like, a **Reference** (such as their
   quotation number) and when the goods or services are **Expected on**.
3. For each line, choose **What it is for** (an expense or asset account), the **Quantity**, the
   **Unit price before GST** and the **GST code** the supplier will charge.
4. Select **Save as a draft**.

Each line's amount is its quantity times its unit price, rounded to the currency's smallest unit (half up). The order
also shows the GST expected and its total.

## Approve an order

Select **Approve** on a saved draft. At or below the self-approval threshold (S$2,000 unless
changed), you approve it alone; in Founder mode it is listed for the weekly review. Above it, select
**Ask for approval**: another person who approves bills, and who did not prepare the order, approves
it in the Inbox. An approved order takes its number (PO-0001 onwards) and never changes: to change
it, close it and raise another.

## Bill an order

When the supplier's invoice arrives, select **Make a bill** on the approved order and say how much of
each line the invoice covers; everything not billed yet is filled in. A draft bill opens: enter the
supplier's **Invoice date** and **Their invoice number**, correct the amounts to the invoice, and post
it as any bill.

- A line billed in parts adds up exactly to its amount: each part takes the cents rounding leaves
  in turn.
- A bill never takes more of a line than is left on the order: posting it is refused.
- The order shows, for each line, what was ordered, what posted bills took and what is left.

## Close an order

Select **Close order** and say why. No more bills are made from it, and a draft bill made from it no
longer posts; the bills posted already stand.

## Common problems

| Message | What to do |
|---|---|
| The order's supplier is not an active vendor (PO-03) | Choose a contact marked as a vendor, or reactivate it |
| A line's quantity or price is not one an order takes (PO-07) | Enter a quantity above zero and a price of zero or more |
| The order needs another person's approval (PO-14) | Select **Ask for approval** |
| You prepared it, so another person approves it (PO-15) | Ask another person who approves bills |
| More than is left of the order (PO-12, PO-13) | Bill at most what is left of each line |
| The order is not approved, or is closed (PO-11) | Bill an approved order that is not closed |

## Related

- [Bills](bills.md)
- [Approvals](approvals.md)
