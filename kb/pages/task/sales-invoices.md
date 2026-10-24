---
key: sales.invoices
type: task
title: Invoices
summary: Entering, issuing and approving an invoice to a customer; emailing it with its PDF; credit notes; spreading revenue; voiding or editing an invoice
sources:
  - docs/user-guide/invoices.md sha256:e205377f4aeeb1bd85e3d2d9d43b3afb47c1c5c040912dd3b131bfb5577923f0
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-25
aliases:
  - invoice
  - sales invoice
  - tax invoice
  - bill a customer
  - new invoice
  - issue an invoice
  - send an invoice
  - email an invoice
  - invoice PDF
  - secure link
  - what a customer owes
  - accounts receivable
  - overdue invoice
  - invoice approval
  - void an invoice
  - edit an invoice
  - credit note
  - customer credit
  - discount on a line
  - deferred revenue
  - spread revenue
  - invoice attachment
  - customer purchase order
  - recurring invoice
  - subscription invoice
  - monthly invoice
  - weekly invoice
  - repeating invoice
  - auto-send
  - issue and send
  - record a payment
  - customer paid
  - receipt
  - payment received
  - short payment
  - overpayment
  - settlement tolerance
---

# Invoices

## What it is

- **Sales → Invoices** keeps each invoice to a customer, with its lines and GST, and each credit note; issuing an invoice adds its total to what the customer owes the company.
- Its states are separate: **Draft**, **Awaiting approval** or **Rejected**; **Issued** or **Voided**; **Sent**, **Delivered**, **Viewed** or **Bounced** from its latest email; **Part paid**, **Paid** or **Written off**; and **Overdue** once its due date has passed while it is not paid in full.
- Reminders of overdue invoices and statements of account are under **Sales → Statements and reminders** ([[sales.statements]]).
- A Bookkeeper, an AR specialist or an Accountant prepares invoices; an AR specialist or an Accountant issues, emails, voids and edits them; the Owner, the Auditor and the Tax agent can see them.

(src: docs/user-guide/invoices.md#what-it-is)

## Enter an invoice

- **New invoice**: choose the **Customer** (a contact marked as a customer), enter their reference and the **Invoice date**; leave the **Due date** blank for the customer's payment terms, or the organisation's.
- Each line takes an **Item** or a description, a **Quantity**, a **Unit price before GST**, a **Revenue account** and a **GST code** such as SR; **Save as a draft** shows the totals and GST as the server works them out ([[contacts.manage]]).
- An invoice can also be made from an accepted quote ([[sales.quotes]]).
- In Expert mode a line can take a **Discount**, a percentage off its quantity × price before GST. An invoice in another currency takes the rate for its date unless one is typed ([[accounting.currencies]]).

(src: docs/user-guide/invoices.md#enter-an-invoice)

## Check and issue

- A draft lists what stops it being issued (✖) and its warnings (⚠), such as a tax invoice dated more than 30 days before today. **Issue** numbers it from the invoice sequence and posts it on its date.
- Basic mode says in one sentence what it adds to what the customer owes; Expert mode shows the ledger impact.

(src: docs/user-guide/invoices.md#check-and-issue-an-invoice)

## Email it

- **Email** on an issued invoice sends it from the invoices sender to the customer's billing email, or an address given, with its PDF and a secure link the customer opens without signing in; the PDF is kept with the invoice and **Emails** lists each send and its delivery.
- A tax invoice missing a particular IRAS requires, such as the customer's address, is not sent until the detail is added ([[settings.templates]]).

(src: docs/user-guide/invoices.md#email-an-invoice)

## Record a payment

- Until bank import matches payments, **Record a payment** on an issued invoice records what the customer paid by hand: the **Date received**, the account **Paid into** and the **Amount received**; it posts at once and the invoice shows **Part paid** or **Paid**.
- Paid more than is open, the rest stays as the customer's credit, never as income; paid a little less, **Settle the invoice in full** clears the rest as a settlement difference within the tolerance, S$1.00 by default, while a larger shortfall stays open or is written off.
- In another currency, an account in that currency takes the bank's or the day's rate; an SGD account needs what it received in SGD.

(src: docs/user-guide/invoices.md#record-a-payment)

## Recurring invoices

- **Sales → Recurring** keeps invoices that repeat: every week or every two weeks on the start's weekday, or every 1, 3, 6 or 12 months on a day of the month, the 29th to the 31st falling on a shorter month's last day; a start in the past makes the invoices of the dates already passed.
- Each occurrence's draft is made on its date for a person to issue, never approved by whoever wrote its lines; **Check** shows the next date and any problem without saving, **Pause** and **Resume** stop and restart it (skipping missed dates), and a change of lines is a new version.

(src: docs/user-guide/invoices.md#recurring-invoices)

## Issued and sent by the scheduler

- Set to **Issue and send** under **What the scheduler does**, the scheduler issues and emails each invoice itself, but only once another person (not the one who wrote the lines) approves the content through **Ask for approval**; each is listed for the weekly review, and a change of content needs the approval again.

(src: docs/user-guide/invoices.md#invoices-the-scheduler-issues-and-sends)

## Approval

- An invoice above the self-approval threshold is not issued by the person issuing it: **Ask for approval**, and another person who approves invoices, and did not prepare it, approves it in the Inbox ([[approvals.decide]]).

(src: docs/user-guide/invoices.md#invoices-above-the-self-approval-threshold)

## Void or edit

- An issued invoice is never changed in place: **Void** posts a reversing entry with a reason, and **Edit** reverses its entry and posts the new version in one step, keeping its number.
- One that receipts or credits settle is voided or edited only once they are voided; in Founder mode another founder approves a void or an edit first (**Ask to void**, **Ask to edit**).

(src: docs/user-guide/invoices.md#void-or-edit-an-issued-invoice)

## Credit notes

- **New credit note** gives a customer credit, linked under **Invoice it credits** to the issued invoice it corrects or standalone; a linked one adjusts GST at that invoice's date's rates and credits at most its total, and is numbered from the credit note sequence.

(src: docs/user-guide/invoices.md#credit-notes)

## Spread revenue

- In Expert mode a line can be spread over 1 to 120 months: issuing puts its revenue on Deferred revenue, and one part a month is released to its account; months still to come are projections, not in the books.

(src: docs/user-guide/invoices.md#spread-revenue-over-months)

## Evidence

- **Evidence** keeps what the invoice rests on, such as the customer's purchase order, and each PDF sent; a file comes off a draft again, but once issued its files are kept and never deleted for as long as the records are.

(src: docs/user-guide/invoices.md#attach-evidence)

## Common problems

- INVOICE-05: choose an active customer. INVOICE-06: choose a code for supplies (SR, ZR, ES or OS). INVOICE-07: a due date on or after the invoice date, or blank.
- INVOICE-09: a quantity and a price with up to 6 decimals, a discount from 0 to 100. INVOICE-10: **Ask for approval**. INVOICE-13: void its receipts and credit applications first. INVOICE-15: an issued invoice of the same customer and currency, for at least the credit.
- RECEIPT-05: record at most what is open. RECEIPT-11: leave it open or write the rest off. SALESDOC-04: add the customer's billing email or give an address. SALESDOC-05: add the missing detail in Settings › Organisation or on the customer.

(src: docs/user-guide/invoices.md#common-problems)
