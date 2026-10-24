---
key: sales.invoices
title: Invoices
mode: basic
roles: [bookkeeper, ar_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AR-03, FR-AR-04, FR-AR-05, FR-AR-06, FR-GL-03, FR-GL-12, FR-DOC-05, RND-LINE, RND-GST-EXCL, FM-1, FM-2, SOD-3]
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
- **Overdue** once its due date has passed while it is not paid in full.

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
with its delivery. Sending again sends the same version again.

A tax invoice that lacks a particular IRAS requires, such as the customer's address, is not sent:
add the detail, then send it again.

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
rates of that invoice's date and credits at most its total. It is entered, saved and issued as an
invoice is, numbered from the credit note sequence, and it takes what it credits off what the
customer owes.

## Spread revenue over months

In Expert mode a line can be spread over 1 to 120 months, from the month the service starts. Issuing
puts its revenue on Deferred revenue, and one part a month is released to the line's account. Months
still to come are projections, not in the books.

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
| Receipts or credits settle the invoice (INVOICE-13) | Void its receipts and credit applications first, then void or edit it |
| The credit note cannot credit that invoice (INVOICE-15) | Choose an issued invoice of the same customer and currency, for at least the credit |
| The customer has no billing email (SALESDOC-04) | Add the customer's billing email, or give the address to send it to |
| The tax invoice lacks a particular IRAS requires (SALESDOC-05) | Add the missing detail in Settings › Organisation or on the customer, then send it again |

## Related

- [Quotes and orders](quotes.md)
- [Contacts and items](contacts-and-items.md)
- [Document templates](document-templates.md)
- [Approvals](approvals.md)
- [Exchange rates](currencies.md)
