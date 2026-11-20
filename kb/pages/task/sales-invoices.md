---
key: sales.invoices
type: task
title: Invoices
summary: Entering, issuing and approving an invoice to a customer; emailing it with its PDF; credit notes; spreading revenue; voiding or editing an invoice
sources:
  - docs/user-guide/invoices.md sha256:8ed1eaab6955e2ada9c63f4f25776dc2db0d715fd200e4c6da6f0b3d12e70d77
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-06
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
  - department on an invoice
  - project on an invoice
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
  - likely paid
  - incoming-funds alert
  - payment link
  - customer portal
  - disputed invoice
  - Stripe subscription
  - subscription contract
  - contracted MRR
  - provider invoice
  - billing provider
  - promise to pay
  - dispute an invoice
  - customer statement online
  - pay online
  - HitPay link
  - link mismatch
---

# Invoices

## What it is

- **Sales → Invoices** keeps each invoice to a customer, with its lines and GST, and each credit note; issuing an invoice adds its total to what the customer owes the company.
- Its states are separate: **Draft**, **Awaiting approval** or **Rejected**; **Issued** or **Voided**; **Sent**, **Delivered**, **Viewed** or **Bounced** from its latest email; **Part paid**, **Paid** or **Written off**; **Overdue** once its due date has passed while it is not paid in full; and **Likely paid** while a bank's incoming-funds alert for its amount, from its customer, waits for the statement line.
- **Likely paid** is a hint only: the invoice stays open and nothing is posted until the line is reconciled to it; an alert expires after 7 days by default, or someone dismisses it ([[settings.organisation]]).
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
- Sending it again sends the very copy kept when it was first sent, the same PDF with the same fingerprint, even if a template has changed since; an edited invoice's new version is made afresh.
- A tax invoice missing a particular IRAS requires, such as the customer's address, is not sent until the detail is added ([[settings.templates]]).

(src: docs/user-guide/invoices.md#email-an-invoice)

## Record a payment

- Until bank import matches payments, **Record a payment** on an issued invoice records what the customer paid by hand: the **Date received**, the account **Paid into** and the **Amount received**; it posts at once and the invoice shows **Part paid** or **Paid**.
- Paid more than is open, the rest stays as the customer's credit, never as income; paid a little less, **Settle the invoice in full** clears the rest as a settlement difference within the tolerance, S$1.00 by default, while a larger shortfall stays open or is written off.
- In another currency, an account in that currency takes the bank's or the day's rate; an SGD account needs what it received in SGD.

(src: docs/user-guide/invoices.md#record-a-payment)

## A payment link on an invoice

- With HitPay connected and a provider account set up, **Add a payment link** on an issued invoice (an Accountant or an AR specialist) asks HitPay for a page that collects what is open, with the invoice's number as its reference; copy or open the link.
- The amount is always the invoice's open balance, read when the link is made, never typed; a draft, void or paid invoice has no link, and an invoice has one live link at a time.
- When the customer pays, **Reconcile now** records the receipt on the invoice once. Another amount, a second payment or a payment on a cancelled link stays in the queue as **link mismatch** for a person, and a payment above the per-line limit (S$500 by default) waits for a person too. A link shows as Expired after its date and as Paid once the invoice has nothing open.
- **Cancel the link** closes it here with a reason; HitPay's page stays payable until it expires. A foreign-currency invoice needs a provider account in that currency; the PayNow QR image and Stripe payment links are not built yet.
- An invoice (or reminder) emailed while it has a live payment link ends with "You can pay this invoice online:" and the stored link, so sending asks the provider for nothing; a cancelled, paid or expired link is left out, and an Owner can switch it off (**Put an invoice's payment link in its email**). With **Make a payment link when an invoice is issued** on (an Administrator switches it on), any issue (by a person, by an approval, or by a recurring invoice on its day; not an invoice mirrored from a billing provider) queues a job that asks the provider for the link outside the request and stores it, so an invoice emailed after that carries it.
- The PDF of an invoice, emailed or downloaded, ends its payment details with "Pay online:" and the stored link while the invoice has a live link (a template that places `{{payment.link}}` itself is not told twice; the PDF emailed is kept as sent, so a later link goes on the next send's PDF). An Owner can switch it off (**Put an invoice's payment link on its PDF**); a quote, credit note or statement never carries it.

(src: docs/user-guide/invoices.md#a-payment-link-on-an-invoice)

## Recurring invoices

- **Sales → Recurring** keeps invoices that repeat: every week or every two weeks on the start's weekday, or every 1, 3, 6 or 12 months on a day of the month, the 29th to the 31st falling on a shorter month's last day; a start in the past makes the invoices of the dates already passed.
- Each occurrence's draft is made on its date for a person to issue, never approved by whoever wrote its lines; **Check** shows the next date and any problem without saving, **Pause** and **Resume** stop and restart it (skipping missed dates), and a change of lines is a new version.

(src: docs/user-guide/invoices.md#recurring-invoices)

## Issued and sent by the scheduler

- Set to **Issue and send** under **What the scheduler does**, the scheduler issues and emails each invoice itself, but only once another person (not the one who wrote the lines) approves the content through **Ask for approval**; each is listed for the weekly review, and a change of content needs the approval again.

(src: docs/user-guide/invoices.md#invoices-the-scheduler-issues-and-sends)

## What the customer sees

- The email's secure link opens the invoice or quote without signing in: the document as its PDF shows it, the amount due or total, and **Download the PDF**; on a sent quote still valid, the customer accepts with their name or declines, with a comment kept as a record ([[sales.quotes]]). It shows nothing else, works until it ends or is revoked, and the first opening marks the invoice **Viewed**.

(src: docs/user-guide/invoices.md#what-the-customer-sees)

## Subscription contracts

- Under **Sales › Subscriptions** (Expert mode), **Your subscription contracts** records a customer's subscription: a name, the currency, the monthly value, the start day and an optional agreed end (an Accountant or AR specialist).
- **Amend** records an upgrade or downgrade (a new monthly value from a day), a cancellation or a renewal after it ended; each day is on or after the start, after the last amendment and in an open month (a closed month is never rewritten).
- The page shows each contract's status and monthly value today and the contracted MRR by currency (the contracts in force, not converted), which is not the revenue booked in the ledger. Contracts and amendments are records, not postings. **Files** on a contract row attaches the signed order form, the customer's email or an amendment letter (optionally naming the amendment); a file is committed at once and cannot be changed or removed. A separate "Converted into" figure adds the totals in the base currency, each currency converted once at the day's rate, shown with each rate and its date; a currency with no rate is named and left out.

(src: docs/user-guide/invoices.md#subscription-contracts)

## Subscriptions a provider bills

- Name a revenue account on a Stripe provider account (Banking › Provider payouts) and Fyniq mirrors the provider's customers, subscriptions and invoices read-only on **Sales › Subscriptions** (Expert mode), with the provider's status, payment attempts and next retry beside the invoice Fyniq made.
- An open or paid invoice becomes a draft invoice, dated the day the provider finalised it, to a contact made from the provider's customer, each line at the provider's amount spread over its service period's months. **Issue the drafts** (also done by Reconcile now) issues one only when its total, GST included, is the provider's to the cent; any other, or one with a discount or credit line, waits for a person with the reason shown.
- The provider's charge for the invoice is received once by Reconcile now (a charge above S$500 waits for a person). Fyniq never retries or charges and its reminders skip these invoices. A provider-voided or uncollectible invoice you issued is voided or written off under Invoices. The provider's credit notes against these invoices are mirrored too: once the invoice is issued each becomes a draft credit note of it at the provider's amounts, and Issue the drafts issues it only when its total, GST included, is the provider's to the cent (one with a discount or a different total waits with the reason shown); a credit note books no money, the refund comes from the bank feed [accountant to confirm the GST of a credit note]. Tiered prices are not built yet.

(src: docs/user-guide/invoices.md#subscriptions-a-provider-bills)

## A disputed invoice or a promise to pay

- **Record it** on an issued invoice (an Accountant or an AR specialist) notes that the customer disputes an amount (at most what is open, with why) or promises to pay on a day (from today to 90 days away).
- A dispute pauses the invoice's reminders until you **Resolve** it with how it ended; a promise until its day has passed. AR ageing shows the disputed part apart; the rows still count it.
- At most one open dispute and one open promise per invoice; a record is resolved once. Nothing is posted: correct the invoice with a credit note.

(src: docs/user-guide/invoices.md#a-disputed-invoice-or-a-promise-to-pay)

## The customer portal

- The customer's side opens through the private link their supplier sent (no sign-in): **Your account** lists the invoices still due with **Pay now** where a payment link exists, a **PDF** for each, and a **Statement of account** over a chosen period (30 days by default, never after today).
- It shows only that customer's invoices, is never cached or indexed, and ends on its date or when the supplier withdraws it. Where there is no Pay button, pay as the invoice says; the supplier records it when it arrives.

(src: docs/user-guide/invoices.md#the-customer-portal)

## Approval

- An invoice above the self-approval threshold is not issued by the person issuing it: **Ask for approval**, and another person who approves invoices, and did not prepare it, approves it in the Inbox ([[approvals.decide]]).

(src: docs/user-guide/invoices.md#invoices-above-the-self-approval-threshold)

## Void or edit

- An issued invoice is never changed in place: **Void** posts a reversing entry with a reason, and **Edit** reverses its entry and posts the new version in one step, keeping its number.
- One that receipts or credits settle is voided or edited only once they are voided; in Founder mode another founder approves a void or an edit first (**Ask to void**, **Ask to edit**).

(src: docs/user-guide/invoices.md#void-or-edit-an-issued-invoice)

## Credit notes

- **New credit note** gives a customer credit, linked under **Invoice it credits** to the issued invoice it corrects or standalone; a linked one adjusts GST at that invoice's date's rates, and it is numbered from the credit note sequence.
- Together, the issued credit notes of one invoice credit at most its total before any deposit it deducts; voiding one frees what it credited.

(src: docs/user-guide/invoices.md#credit-notes)

## Spread revenue

- In Expert mode a line can be spread over 1 to 120 months: issuing puts its revenue on Deferred revenue, and one part a month is released to its account; months still to come are projections, not in the books.

(src: docs/user-guide/invoices.md#spread-revenue-over-months)

## Dimensions on a line

- When the organisation uses dimensions, such as Department or Project, each line can take one value of each, such as ENG Engineering, so its revenue shows by team or by project ([[accounting.chart]]).
- The choices are in Expert mode; a line on a revenue account that requires a dimension shows its choice in Basic mode too, with "needs one" under it, and the invoice does not issue until the line has one (DIM-05).
- A spread line's revenue keeps its values each month it is released; a credit note's lines take values in the same way, and a quote's and a recurring invoice's lines pass theirs to the invoices made from them.
- In Basic mode, an invoice whose lines have values in Expert's fields (a discount, a spread, a dimension value the account does not require) shows **Has advanced details** with how many: open it to read them, **Open in Expert mode** to change them.

(src: docs/user-guide/invoices.md#tag-a-line-with-dimensions)

## Evidence

- **Evidence** keeps what the invoice rests on, such as the customer's purchase order, and each PDF sent; a file comes off a draft again, but once issued its files are kept and never deleted for as long as the records are.

(src: docs/user-guide/invoices.md#attach-evidence)

## Common problems

- INVOICE-05: choose an active customer. INVOICE-06: choose a code for supplies (SR, ZR, ES or OS). INVOICE-07: a due date on or after the invoice date, or blank.
- INVOICE-09: a quantity and a price with up to 6 decimals, a discount from 0 to 100. INVOICE-10: **Ask for approval**. DIM-05: choose the line's value, such as its department. DIM-06: one value in use of each dimension, or None. INVOICE-13: void its receipts and credit applications first. INVOICE-15: an issued invoice of the same customer and currency, for at least the credit; its other credit notes may already credit its total, so credit less or void one of them.
- RECEIPT-05: record at most what is open. RECEIPT-11: leave it open or write the rest off. SALESDOC-04: add the customer's billing email or give an address. SALESDOC-05: add the missing detail in Settings › Organisation or on the customer.

(src: docs/user-guide/invoices.md#common-problems)
