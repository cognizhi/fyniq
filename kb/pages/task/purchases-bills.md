---
key: purchases.bills
type: task
title: Bills
summary: Entering, posting and approving a supplier's bill; claiming its GST; recurring bills; spreading a prepaid cost; payments; voiding or editing a bill
sources:
  - docs/user-guide/bills.md sha256:ca3ba19c22b752c23aebc6cbbf19bd065d326a6209886dc7da3d4232c213308e
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
  - claim GST
  - simplified tax invoice
  - receipt GST
  - business share
  - private use
  - employee's invoice
  - invoice in a director's name
  - reimbursed expense GST
  - tax agent decision
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
  - recurring bill
  - monthly rent bill
  - repeating bill
  - attach the invoice
  - bill attachment
  - supplier invoice file
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

## Attach the supplier's invoice

- Under **Evidence** on a saved bill or credit note: choose the file, say what it is (such as the invoice or a receipt) and select **Attach**. PDF, image, CSV, text, XLSX, DOCX and email files of up to 25 MB are accepted; Fyniq scans each one and strips image metadata such as location.
- While the bill is a draft a file can come off again; once it is posted its files are kept with it and never deleted, for as long as the records are, and a file added later is kept the same way.

(src: docs/user-guide/bills.md#attach-the-suppliers-invoice)

## Check and post

- A saved draft lists what stops it posting (✖) and its warnings (⚠): a stated GST that differs from Fyniq's own working of it even by a cent, another bill from the supplier with the same number and total within 60 days, and a bill in another currency with no GST stated in SGD (its GST stays in the cost).
- Expert mode shows the **Ledger impact**; Basic mode says how much the bill adds to what the company owes the supplier. **Post** posts it on its date, numbered from the bill sequence (BILL-0001 onwards).
- Input tax is claimed only once the company is GST-registered on the bill's date, for codes such as TX; until then the GST is part of the cost. **Input tax** below says what else decides it.

(src: docs/user-guide/bills.md#check-and-post-a-bill)

## Input tax

- Whether a bill's GST is claimed is decided line by line from what the bill records, never from the GST amount alone. Under **Input tax** the bill says **What the claim rests on** (a **Tax invoice** addressed to the company, "Company c/o another" counting; a **Simplified tax invoice**, which needs no customer name; an **Import permit** naming the company; or **No valid tax invoice**), whether **the supplier is GST-registered** (its GST number is on the document) and whom it is **addressed to**; each line takes a **Business share (%)**, blank for all of it.
- Each line shows **Claimable** with its reason; **Claimed for 60%** (or another share), where only the business share of the GST is claimed, rounded half up to the cent, the rest staying in the cost; **Not claimable** with its reason (not GST-registered on the bill's date, a code such as BL for a motor car or club fees, an unregistered supplier, no valid tax invoice, or a business share of 0); or **For the tax agent** (⚠), not claimed until they decide.
- A simplified tax invoice is claimable while the bill's total, GST included, is at most S$1,000.00, the limit in the GST pack from 23 Oct 2026; above it, ask the supplier for a tax invoice. One dated before 23 Oct 2026 goes to the tax agent, as the pack does not record the limit before then.
- An invoice in an employee's or director's name, bought for the company and paid back: choose **An employee or director** under **Addressed to**, bill what the company paid back (not the whole invoice) with the GST inside that amount (at 9% GST, 9/109 of it), and attach the invoice and the record of the repayment; until a file is attached the bill shows ⚠. Credited to their director's loan instead (**Credited to their director's loan**), the GST is not claimed until the tax agent decides.
- The tax agent's decision is recorded under **The tax agent's decision**, with their advice attached; on a posted bill, **Edit**, record it and post the edit, and the claim posts with it.

(src: docs/user-guide/bills.md#input-tax)

## Spread a cost over months

- A cost paid ahead, such as a year's subscription or insurance, is spread over the months it covers: in Expert mode a line takes **Spread over** (1 to 120 months) and **Spread from** (the month the service starts, blank for the invoice's month); a line with a spread shows both in Basic mode too.
- Posting puts the line's cost in **1210 Prepayments**, in SGD, and claims the GST in full at once; each month one even part moves to the line's account, the rounding cents falling in different months so the parts add up exactly to the cost.
- A part posts on the first day of its month, or on the invoice date for the months before it; Fyniq posts it by itself once its date comes, in the company's time zone, never a month ahead, and a part whose month has no open period waits until it opens.
- **Spread over months** on the bill lists each part as posted, due but not posted yet, or projected (not in the books). Voiding the bill reverses the posted parts on the void's date; editing reverses them on their own dates and posts the new version's.
- Only an expense line's cost is spread, and never a credit note's lines.

(src: docs/user-guide/bills.md#spread-a-cost-over-months)

## Recurring bills

- A bill that comes every month, quarter or year, such as the office rent, is set up once under **Purchases → Bills → Recurring** with **New recurring bill**: its name, how often it repeats (every month, 3 months, 6 months or year) and on which day, when it starts and ends, then the supplier, currency and lines as on a bill; the 29th to the 31st fall on the last day of a shorter month.
- Fyniq makes a draft bill on each date by itself, within the hour; nothing recurring posts by itself: add **Their invoice number** from the supplier's invoice, check the draft and post it as any bill. Whoever wrote a recurring bill's lines does not approve the posting of its drafts.
- **Check** shows the next draft's date and what would stop the drafts posting, without saving; a recurring bill whose lines a bill would refuse is kept and shows ✖ until fixed. A change applies to drafts made after it; **Pause** stops the drafts, and **Resume** starts them again from today, skipping the dates passed.
- At the month's close, Fyniq warns of a recurring bill due in the month with no draft yet, and refuses the close when one is more than a day late.

(src: docs/user-guide/bills.md#recurring-bills)

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
- BILL-14, an input-tax fact is not one Fyniq reads: choose what the claim rests on and whom it is addressed to from the lists; say how an agent was paid back only for an agent's invoice.
- BILL-15, a line's business share is not a percentage: enter the share used for the business from 0 to 100, such as 60, or leave it blank.

(src: docs/user-guide/bills.md#common-problems)

## Related

- [[purchases.orders]], [[accounting.currencies]], [[settings.organisation]], [[contacts.manage]], [[approvals.decide]]

(src: docs/user-guide/bills.md#related)
