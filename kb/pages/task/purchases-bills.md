---
key: purchases.bills
type: task
title: Bills
summary: Entering, posting and approving a supplier's bill; spreading a prepaid cost over months; recording and voiding payments; voiding or editing a bill
sources:
  - docs/user-guide/bills.md sha256:b202b41117902877625f52fe091bcad1f9224ed4a73530cd5efc74923c9f2f29
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
  - AP ageing
  - what we owe suppliers
  - bills due
  - cash requirement
  - credit note
  - vendor credit
  - supplier credit
  - apply a credit
  - supplier refund
  - spread a cost
  - prepayment
  - prepaid subscription
  - spread over months
---

# Bills

## What it is

- **Purchases → Bills** keeps each supplier's invoice with its lines and GST; posting a bill adds its total to what the company owes the supplier.
- Its states are separate: **Draft**, **Awaiting approval** or **Rejected**; **Posted** or **Voided**; **Part paid** or **Paid** once payments are recorded against it; and **Overdue** once its due date has passed while it is not paid in full.
- A Bookkeeper, an AP specialist or an Accountant enters bills; an Accountant posts, voids and edits them; the Owner, the Auditor and the Tax agent can see them.

(src: docs/user-guide/bills.md#what-it-is)

## What the company owes

- At the top of **Purchases → Bills**, **What we owe suppliers** gives, in each currency, what the bills due in the next 30 days need and how much of it is overdue; a bill falls due on its due date, or by the payment terms when it has none. Credits with suppliers are not taken off: they are applied to a bill first.
- In Expert mode it also shows the ageing: what is owed by how long it is past due (not yet due, 1–30, 31–60, 61–90 and over 90 days), with the credits apart.

(src: docs/user-guide/bills.md#what-the-company-owes)

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

## Spread a cost over months

- A cost paid ahead, such as a year's subscription or insurance, is spread over the months it covers: in Expert mode a line takes **Spread over** (1 to 120 months) and **Spread from** (the month the service starts, blank for the invoice's month); a line with a spread shows both in Basic mode too.
- Posting puts the line's cost in **1210 Prepayments**, in SGD, and claims the GST in full at once; each month one even part moves to the line's account, the rounding cents falling in different months so the parts add up exactly to the cost.
- A part posts on the first day of its month, or on the invoice date for the months before it; Fyniq posts it by itself once its date comes, in the company's time zone, never a month ahead, and a part whose month has no open period waits until it opens.
- **Spread over months** on the bill lists each part as posted, due but not posted yet, or projected (not in the books). Voiding the bill reverses the posted parts on the void's date; editing reverses them on their own dates and posts the new version's.
- Only an expense line's cost is spread, and never a credit note's lines.

(src: docs/user-guide/bills.md#spread-a-cost-over-months)

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

## Credit notes

- A supplier's credit note is entered with **New credit note** under **Purchases → Bills**, line by line with its GST, as a bill is; posting it takes it off what the company owes the supplier, numbered from its own sequence (VC-0001 onwards). The list's **Credit notes** shows them.
- **Apply to a bill** settles one of the supplier's open bills in its currency, both going down by the amount: nothing posts in SGD, and in another currency the difference between the two rates is the exchange gain or loss. **Record refund** records money the supplier paid back, posted at once and numbered from the refund sequence (RF-0001 onwards).
- What a payment paid beyond a bill is a credit too, applied or refunded the same way. A credit note shows **Part applied** or **Applied** and what is left; each application or refund is voided under **Payments**, and a credit note is not voided or edited while applied or refunded.

(src: docs/user-guide/bills.md#credit-notes)

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
- CREDIT-05, more than the bill has open or the credit has left: apply or refund at most what is open on both.
- CREDIT-08, the credit is applied to bills: void its applications and refunds first.
- LEDGER-28, the spread cannot be used: spread an expense line over 1 to 120 months; a credit note's lines are not spread.

(src: docs/user-guide/bills.md#common-problems)

## Related

- [[accounting.currencies]], [[settings.organisation]], [[contacts.manage]], [[approvals.decide]]

(src: docs/user-guide/bills.md#related)
