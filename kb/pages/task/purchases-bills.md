---
key: purchases.bills
type: task
title: Bills
summary: Entering, posting and approving a supplier's bill and its warnings; recording and voiding its payments; voiding or editing a posted bill
sources:
  - docs/user-guide/bills.md sha256:5fb5729dd5c118537bbf4ed15b94a59fb083b47d590f2598681cc1af87b20535
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-23
aliases:
  - bill
  - supplier invoice
  - vendor bill
  - enter a bill
  - post a bill
  - accounts payable
  - what we owe a supplier
  - input tax
  - GST on a bill
  - duplicate bill
  - void a bill
  - edit a bill
  - bill approval
  - pay a bill
  - record a payment
  - part payment
  - bill paid
  - void a payment
---

# Bills

## What it is

- **Purchases → Bills** keeps each supplier's invoice with its lines and GST; posting a bill adds its total to what the company owes the supplier.
- Its states are separate: **Draft**, **Awaiting approval** or **Rejected**; **Posted** or **Voided**; **Part paid** or **Paid** once payments are recorded against it; and **Overdue** once its due date has passed while it is not paid in full.
- A Bookkeeper, an AP specialist or an Accountant enters bills; an Accountant posts, voids and edits them; the Owner, the Auditor and the Tax agent can see them.

(src: docs/user-guide/bills.md#what-it-is)

## Enter a bill

- **New bill**: choose the **Supplier** (a contact marked as a vendor), enter **Their invoice number** and the **Invoice date**; leave the **Due date** blank for the supplier's payment terms, or the company's (30 days unless changed).
- Each line takes **What it is for** (an expense or asset account), the invoice's **GST code**, the **Amount before GST** and the **GST as stated**; **Save as a draft** keeps it out of the books.
- A bill in another currency takes the rate for its invoice date unless one is typed ([[accounting.currencies]]), and its **GST stated in SGD**, as the tax invoice states it, is the input tax claimed.

(src: docs/user-guide/bills.md#enter-a-bill)

## Check and post

- A saved draft lists what stops it posting (✖) and its warnings (⚠): a stated GST that differs from Fyniq's own working of it even by a cent, another bill from the supplier with the same number and total within 60 days, and a bill in another currency with no GST stated in SGD (its GST stays in the cost).
- Expert mode shows the **Ledger impact**; Basic mode says how much the bill adds to what the company owes the supplier. **Post** posts it on its date, numbered from the bill sequence (BILL-0001 onwards).
- Input tax is claimed only once the company is GST-registered on the bill's date, for codes such as TX; until then the GST is part of the cost.

(src: docs/user-guide/bills.md#check-and-post-a-bill)

## Approval above the threshold

- A bill whose total is above the self-approval threshold (S$2,000 unless changed) is asked for with **Ask for approval**: another person who approves bills, and did not prepare it, approves it in the Inbox, and it posts then.
- Below the threshold, in Founder mode, a founder posts it alone and it is listed for the weekly review.

(src: docs/user-guide/bills.md#bills-above-the-self-approval-threshold)

## Record a payment

- Until bank import matches payments, a payment the company made is recorded by hand: on the posted bill, **Record payment**, then the **Date paid**, the account it was **Paid from** (a bank, card or wallet account in the bill's currency or in SGD), the **Amount paid of the bill** (it starts at what is still open; less records a part payment) and the **Bank reference**.
- It posts at once, numbered from the payment sequence (PAY-0001 onwards): what the company owes the supplier goes down, and so does the account it was paid from; the bill shows **Part paid** or **Paid**, what is still open, and its payments. A payment never settles more than is open.
- A bill in another currency paid from an account in that currency takes the rate for the date paid, or the bank advice's rate; paid from an SGD account, enter **What the account paid** in SGD as the statement shows it.
- What the payment cost against what the bill carried at its own rate is the exchange gain or loss, posted to FX & rounding differences; the last part of a bill paid in parts clears the cent of rounding the parts leave.

(src: docs/user-guide/bills.md#record-a-payment)

## Void a payment

- A payment recorded by mistake is voided, never deleted: **Void** beside it under **Payments**, with a date and a reason, posts a reversing entry and the bill is open again by what the payment paid; in Founder mode another founder approves it first (**Ask to void**).
- A bill that payments settle is not voided or edited until its payments are voided.

(src: docs/user-guide/bills.md#void-a-payment)

## Void or edit a posted bill

- A posted bill is never changed in place: **Void** posts a reversing entry on a chosen date, with a reason, and the bill stays marked voided; **Edit** reverses its entry and posts the new version in one step, with a reason, keeping its number.
- In Founder mode another founder approves a void or an edit first (**Ask to void**, **Ask to post the edit**).

(src: docs/user-guide/bills.md#void-or-edit-a-posted-bill)

## Common problems

- BILL-05, the supplier is not an active vendor: choose a contact marked as a vendor, or reactivate it.
- BILL-06, not a GST code for purchases: choose the invoice's code, such as TX, BL or NR.
- BILL-07, the due date is before the bill's date: enter a later date, or leave it blank.
- BILL-09, another person's approval is needed: select **Ask for approval**.
- LEDGER-22, a system account is missing: load the standard chart, with 2010 Accounts payable and 2200 GST control.
- BILLPAY-05, more than the bill has open: enter at most what is open, and void a payment entered twice.
- LEDGER-27, what the account paid is missing: for a bill in another currency paid from an SGD account, enter what it paid in SGD.
- BILL-13, payments settle the bill: void its payments first, then void or edit the bill.

(src: docs/user-guide/bills.md#common-problems)

## Related

- [[accounting.currencies]], [[settings.organisation]], [[contacts.manage]], [[approvals.decide]]

(src: docs/user-guide/bills.md#related)
