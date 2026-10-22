---
key: purchases.bills
type: task
title: Bills
summary: Entering a supplier's bill, the warnings it posts with, posting it, approval above the threshold, and voiding or editing a posted bill
sources:
  - docs/user-guide/bills.md sha256:d65f668474a8cbc85f67eb6d6ee28fb8b4c35df85350bd6451017c812d8c449e
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-22
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
---

# Bills

## What it is

- **Purchases → Bills** keeps each supplier's invoice with its lines and GST; posting a bill adds its total to what the company owes the supplier.
- Its states are separate: **Draft**, **Awaiting approval** or **Rejected**; **Posted** or **Voided**; and **Overdue** once its due date has passed while it is unpaid.
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

(src: docs/user-guide/bills.md#common-problems)

## Related

- [[accounting.currencies]], [[settings.organisation]], [[contacts.manage]], [[approvals.decide]]

(src: docs/user-guide/bills.md#related)
