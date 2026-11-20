---
key: sales.invoices
title: Invoices
mode: basic
roles: [bookkeeper, ar_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AR-03, FR-AR-04, FR-AR-05, FR-AR-06, FR-GL-03, FR-GL-12, FR-DOC-05, FR-BNK-16, RND-LINE, RND-GST-EXCL, FM-1, FM-2, SOD-3]
status: draft            # written with the sales screens (P1, p1-sales task 8.1); reviewed with the accountant
---

# Invoices

## What it is

**Sales → Invoices** keeps what the company bills its customers: each invoice, with its lines and
GST, and each credit note. Issuing an invoice adds its total to what the customer owes the company.

An invoice has separate states:

- **Draft** until it is issued; **Awaiting approval** while another person is asked to approve it;
  **Rejected** when they said no;
- **Issued** once it is in the books, or **Voided** once it is reversed;
- **Sent**, **Delivered**, **Viewed** or **Bounced**, from its latest email;
- **Part paid** or **Paid** once receipts or credits settle it, or **Written off**;
- **Overdue** once its due date has passed while it is not paid in full;
- **Likely paid** while a bank's incoming-funds alert for its amount, from its customer, waits for the
  statement line. It is a hint only: the invoice stays open, and nothing is posted, until the line is
  reconciled to it. An alert expires after 7 days by default, or someone dismisses it.

Reminders of overdue invoices, and statements of account, are under **Sales → Statements and
reminders**.

A Bookkeeper, an AR specialist or an Accountant prepares invoices. An AR specialist or an
Accountant issues, emails, voids and edits them. The Owner, the Auditor and the Tax agent can see
them.

## Enter an invoice

1. Select **New invoice**.
2. Choose the **Customer**: a contact marked as a customer.
3. Enter the customer's reference, such as their purchase order number, the **Invoice date** and,
   if it differs from the payment terms, the **Due date**. Left blank, the due date comes from the
   customer's terms, else the organisation's.
4. For each line, choose an **Item** if it is one you sell, or type the description; enter the
   **Quantity** and the **Unit price before GST**, choose the **Revenue account** and the **GST code**,
   such as SR for a standard-rated supply.
5. Select **Save as a draft**. The draft shows its totals and GST as the server works them out, and
   anything that stops it being issued.

In Expert mode a line can also take a **Discount**, a percentage off its quantity × price before GST.

An invoice in another currency takes the rate for its date unless you type one.

An invoice can also be made from an accepted quote: see [Quotes and orders](quotes.md).

## Check and issue an invoice

The draft lists what stops it being issued (✖) and what it is issued with (⚠), such as a tax
invoice dated more than 30 days before today. Fix each ✖, then select **Issue**: the invoice is
numbered from the invoice sequence and posted on its date. Basic mode says in one sentence what it
adds to what the customer owes; Expert mode shows the ledger impact, debit by debit.

## Email an invoice

Select **Email** on an issued invoice. It goes from the organisation's invoices sender to the
customer's billing email, or the address you give, with the PDF of the invoice and a secure link the
customer opens without signing in. The PDF is kept with the invoice, and **Emails** lists each send
with its delivery. Sending it again sends the very copy kept when it was first sent, the same PDF
with the same fingerprint, even if a template has changed since; an edited invoice's new version is
made afresh.

A tax invoice that lacks a particular IRAS requires, such as the customer's address, is not sent:
add the detail, then send it again.

## Record a payment

Until bank import matches payments by itself, record each payment a customer makes by hand: select
**Record a payment** on an issued invoice, enter the **Date received**, the account it was **Paid into**
and the **Amount received**. It posts at once, and the invoice shows **Part paid** or **Paid**.

- A customer who paid more than is open: the rest stays as their credit, never as income, to apply to
  another invoice or refund.
- A customer who paid a little less, such as after a bank charge: tick **Settle the invoice in full**.
  What is left clears as a settlement difference when it is within the organisation's settlement
  tolerance, S$1.00 by default; a larger shortfall stays open, or is written off.
- An invoice in another currency paid into an account in that currency takes the rate on the bank's
  advice, or the day's rate; paid into an SGD account, enter what the account received in SGD.

## A payment link on an invoice

An organisation with HitPay connected under **Settings › Integrations** and a provider account set up under
**Banking › Provider payouts** can put a payment page on an issued invoice. Open the invoice and select
**Add a payment link** (an Accountant or an AR specialist can): Fyniq asks HitPay for a page that collects
what is open on the invoice, with the invoice's number as its reference, and shows the link to copy or open.

- The amount is always what the invoice has open, read by Fyniq when the link is made; you never type it. A
  draft, void or paid invoice has no link, and an invoice has one live link at a time.
- When the customer pays, HitPay's charge arrives on the provider's clearing account and **Reconcile now**
  (or the hourly one) records the receipt on the invoice by itself, once. A payment of another amount, a
  second payment on the same link, or one on a link you cancelled is not matched: it stays in the queue
  with the reason **link mismatch**, for a person to decide. A payment above the per-line limit of automatic
  reconciliation (S$500 by default) also waits for a person, as every large line does.
- **Cancel the link** closes it in Fyniq, with a reason, so a new one can be made. HitPay's page stays
  payable until it expires, so a payment made on it afterwards waits for a person.
- A link shows as **Expired** after its date (30 days after the invoice's due date) and as **Paid** once the
  invoice has nothing open; an expired link is replaced by making a new one.
- A foreign-currency invoice needs a provider account in that currency. The PayNow QR image of the
  checkout and Stripe payment links are not built yet.

**Emailing an invoice with its link.** An invoice emailed while it has a live payment link ends with a line, "You can pay this invoice online:", and the
link. It is the link already made for the invoice, so sending an email never asks the payment provider for anything: make the link first, then
send the invoice, and a link made after a send goes with the next one (a reminder too). A cancelled, paid or expired link is left out. An
Owner can switch this off with **Put an invoice's payment link in its email** under organisation settings. Making a link by itself when an invoice
is issued, and printing the link on the invoice's PDF, are not built yet.


## Recurring invoices

**Sales → Recurring** keeps invoices that repeat: a name, a schedule, the customer and the lines. A
schedule is every week or every two weeks on the start's weekday, or every 1, 3, 6 or 12 months on a day
of the month; the 29th to the 31st fall on the last day of a shorter month. A start in the past makes the
invoices of the dates already passed.

Each occurrence's draft invoice is made on its date, dated that day. Check it and issue it as any
invoice; whoever wrote the lines of the recurring invoice does not approve the issue of its drafts.
**Check** shows the next invoice's date and anything that would stop its invoices being issued, without
saving. **Pause** stops it until **Resume**, which skips the dates it missed. A change of lines is a new
version: invoices already made keep theirs.

## Invoices the scheduler issues and sends

Under **What the scheduler does**, a recurring invoice can **Issue and send** each invoice itself instead
of making a draft. It does so only once another person approves its content: select **Ask for approval**,
and another person, not the one who wrote the lines, approves it in the Inbox. Each invoice the scheduler
issues is emailed to the customer's billing email and listed for the weekly review. A change of content
needs the approval again; until then, the scheduler makes drafts.

## What the customer sees

The secure link in an email opens the invoice or quote for the customer without signing in: the
document as its PDF shows it, the amount due or the total, and **Download the PDF**. On a sent quote
still valid, the customer accepts it with their name, or declines it, with a comment kept as a record of
the decision. The link shows nothing else, works until it ends or is revoked, and the first time it is
opened the invoice shows **Viewed**.

## Subscription contracts

Under **Sales › Subscriptions** (Expert mode), **Your subscription contracts** records what each customer subscribes to: a name, the currency,
the monthly value, the day it starts and, if agreed, the day it ends. An Accountant or an AR specialist records them.

- **Amend** records an upgrade or downgrade (a new monthly value from a day), a cancellation (the first day with no subscription) or a
  renewal (it starts again after it ended, and runs past its agreed end). Each is dated: the day is on or after the contract's start, after
  its last amendment, and in a month that is still open. A closed month is never rewritten.
- The page shows each contract's status and monthly value today, and the **contracted MRR**: the monthly value of the contracts in force,
  by currency, not converted. It differs from the revenue booked in the ledger by mid-month starts, credits, unpaid or failed invoices and
  exchange rates, so each is named wherever it is shown. Beside it, **Converted into SGD** adds the totals up in the base currency as a figure of its
  own: each currency's total is converted once at the rate of the day (the organisation's rate, else the daily feed's, else the month's), the rate and
  its date are shown, and a currency with no rate for the day is named and left out, so the figure is then not the whole.
- A contract and its amendments are records, not postings: issue the invoices and credit notes as you do, and the revenue follows your
  revenue policy. Evidence for an amendment and the schedule's day-weighted revenue are not built yet.

## Subscriptions a provider bills

If your customers subscribe and pay through a billing provider such as Stripe, name a **revenue account** on its provider account
(under **Banking › Provider payouts**; an Owner or someone with the bank-connections right does it once). Fyniq then mirrors the
provider's customers, subscriptions and invoices, read-only, on **Sales › Subscriptions** (Expert mode): each invoice with what the
provider says (its status, the payment attempts so far, the next retry) beside what Fyniq made of it.

- An open or paid invoice becomes a **draft invoice** by itself, dated the day the provider finalised it, to a contact made from the
  provider's customer the first time, with each line at the provider's amount, spread over the months of its service period.
- **Issue the drafts** (an Accountant or an AR specialist; also done when you press **Reconcile now**) issues a draft only when its
  total, GST included, is the provider's to the cent. Any other, or one with a discount, a credit line or more lines than a page, waits
  for a person with the reason shown; nothing is posted for it.
- When the provider's charge for the invoice arrives, **Reconcile now** records the receipt once. A charge above the per-line limit
  (S$500 by default) waits for a person.
- Fyniq never retries a payment or charges anyone, and its payment reminders do not run on these invoices. If the provider voids an
  invoice you issued, or marks it uncollectible, the page says so: void or write it off under **Invoices**.
- Not built yet: the provider's credit notes, and mixed-currency or tiered prices (they wait for a person).

## A disputed invoice or a promise to pay

When a customer disputes part of an issued invoice, or promises to pay it on a day, record it on the invoice (an Accountant or an AR
specialist): select **Record it** under **Dispute or promise to pay**.

- A **dispute** has the amount in dispute (at most what is open) and why. Its reminders are paused until you **Resolve** it with how it
  ended, such as "credit note issued"; AR ageing then shows the disputed part apart (the rows still count it).
- A **promise** has the day the customer says they will pay, from today to 90 days away. Its reminders are paused until that day has
  passed, or you resolve it sooner.
- An invoice has at most one open dispute and one open promise, and a record is resolved once; record a new one if the customer says
  something new. Nothing is posted: to correct the invoice itself, issue a credit note.

## The customer portal

The customer's side of the portal, opened through the private link their supplier sent them (see **Contacts**, "The customer
portal"), is a page with no sign-in: **Your account** lists the invoices still due with a **Pay now** button where the supplier
made a payment link, a **PDF** for each, and a **Statement of account** over a period you choose (30 days by default, never
ending after today). It shows only that customer's invoices, is never cached or indexed, and the link ends on its date or when the
supplier withdraws it. Where there is no Pay button, pay as the invoice says: the supplier records it when it arrives.

## Invoices above the self-approval threshold

An invoice whose total is above the self-approval threshold is not issued by the person issuing it.
Select **Ask for approval**: another person who approves invoices, and who did not prepare this one,
approves it in the Inbox, and it is issued then.

## Void or edit an issued invoice

An issued invoice is never changed in place.

- **Void** posts a reversing entry on a date you choose, with a reason. The invoice stays in the
  books, marked voided.
- **Edit** reverses the entry the invoice has and posts the new version in one step, with a reason.
  The invoice keeps its number.

An invoice that receipts or credits settle is voided or edited only once they are voided. In Founder
mode another founder approves a void or an edit first: the buttons say **Ask to void** and **Ask to
edit**.

## Credit notes

Select **New credit note** to give a customer credit. Link it to the customer's issued invoice it
corrects under **Invoice it credits**, or leave it standalone. A linked credit note adjusts GST at the
rates of that invoice's date. Together, the issued credit notes of one invoice credit at most its total
before any deposit it deducts, and voiding one frees what it credited. A credit note is entered, saved
and issued as an invoice is, numbered from the credit note sequence, and it takes what it credits off
what the customer owes.

## Spread revenue over months

In Expert mode a line can be spread over 1 to 120 months, from the month the service starts. Issuing
puts its revenue on Deferred revenue, and one part a month is released to the line's account. Months
still to come are projections, not in the books.

## Tag a line with dimensions

When the organisation uses dimensions, such as Department or Project, each line can take one value
of each, such as ENG Engineering, so its revenue shows by team or by project. The choices are in
Expert mode; a line on a revenue account that requires a dimension shows its choice in Basic mode
too, with "needs one" under it, and the invoice does not issue until the line has one (DIM-05).

In Basic mode, an invoice whose lines have values in Expert's fields, such as a discount, a spread or
a dimension value the account does not require, shows **Has advanced details** with how many. Open it
to read them; **Open in Expert mode** to change them.

A spread line's revenue keeps its values each month it is released. A credit note's lines take values
in the same way, and a quote's and a recurring invoice's lines pass theirs to the invoices made from
them.

## Attach evidence

**Evidence** keeps what the invoice rests on, such as the customer's purchase order or a delivery
confirmation, and each PDF sent to the customer. While the invoice is a draft a file can come off
again; once it is issued its files are kept, and never deleted, for as long as the records are.

## Common problems

| Message | What to do |
|---|---|
| The invoice's customer is not an active customer (INVOICE-05) | Choose a contact marked as a customer, or reactivate it |
| Not a GST code for supplies (INVOICE-06) | Choose a code for supplies, such as SR, ZR, ES or OS |
| The due date is before the invoice's date (INVOICE-07) | Enter a due date on or after the invoice date, or leave it blank |
| A line's quantity, price or discount cannot be one (INVOICE-09) | Enter a quantity and a price with up to 6 decimals, and a discount from 0 to 100 |
| The invoice needs another person's approval (INVOICE-10) | Select **Ask for approval** |
| A line's account needs a value of a dimension (DIM-05) | Choose the line's value, such as its department |
| A dimension value that does not exist or is out of use, or two values of one dimension (DIM-06) | Choose one value in use of each dimension, or None |
| Receipts or credits settle the invoice (INVOICE-13) | Void its receipts and credit applications first, then void or edit it |
| More than the invoice has open (RECEIPT-05) | Record at most what is open; the rest is kept as the customer's credit |
| What is left is above the settlement tolerance (RECEIPT-11) | Leave the invoice open, or write the rest off |
| The credit note cannot credit that invoice (INVOICE-15) | Choose an issued invoice of the same customer and currency, for at least the credit. The invoice's other credit notes may already credit its total: credit less, or void one of them |
| The customer has no billing email (SALESDOC-04) | Add the customer's billing email, or give the address to send it to |
| The tax invoice lacks a particular IRAS requires (SALESDOC-05) | Add the missing detail in Settings › Organisation or on the customer, then send it again |

## Related

- [Quotes and orders](quotes.md)
- [Statements and reminders](statements.md)
- [Contacts and items](contacts-and-items.md)
- [Document templates](document-templates.md)
- [Approvals](approvals.md)
- [Exchange rates](currencies.md)
