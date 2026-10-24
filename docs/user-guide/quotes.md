---
key: sales.quotes
title: Quotes and orders
mode: basic
roles: [bookkeeper, ar_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AR-01, FR-AR-02, FR-AR-06, FR-AR-08, FR-AR-16]
status: draft            # written with the sales screens (P1, p1-sales task 8.1); reviewed with the accountant
---

# Quotes and orders

## What it is

**Sales → Quotes and orders** keeps what the company offers its customers and the orders they place.
A quote offers lines at a price until a date; a sales order records an order a customer placed.
Neither posts anything: an accepted one becomes an invoice, and the invoice posts.

A quote is **Draft** until it is sent, then **Sent**, **Accepted** or **Declined** by the customer,
**Expired** once its validity date passes with no answer, **Invoiced** once issued invoices take every
line, or **Cancelled**.

A Bookkeeper, an AR specialist or an Accountant prepares quotes and makes invoices from accepted ones.
An AR specialist or an Accountant sends, emails, records answers to and cancels them.

## Prepare a quote

1. Select **New quote**.
2. Choose the **Customer**, enter their reference, the **Quote date** and **Valid until**: the last day
   the quote is valid, on or after its date.
3. Enter each line as for an invoice: an item or a description, the quantity, the unit price before
   GST, the revenue account and the GST code.
4. Select **Save as a draft**. It shows its totals and GST as the server works them out.

## Send a quote

Select **Send** on a draft: it is numbered from the quote sequence and counts in the pipeline. Then
select **Email** to send it to the customer's billing email, or another address, with its PDF and a
secure link where the customer accepts or declines it online. **Emails** lists each send.

A reminder of a quote about to expire, 3 days before its validity date, waits in the Inbox to be sent
or skipped.

## The customer's answer

A customer who answers through the secure link records it themselves, with their name and comment,
kept as evidence. One who answers by email or phone: select **Record accepted** or **Record declined**,
with the day, who answered and a note.

## Invoice an accepted quote

Select **Make the invoice** on an accepted quote: a draft invoice takes everything not invoiced yet.
Check it and issue it as any invoice. **Invoicing** shows, line by line, what was quoted, what issued
and draft invoices take, and what is left.

## Cancel a quote

Select **Cancel**, with a reason, to withdraw a sent quote or order: it stays in the records, marked
cancelled, and leaves the pipeline. One that issued invoices take is not cancelled.

## Sales orders

Select **New sales order** to record an order a customer placed. It is entered and sent as a quote is,
numbered from the sales order sequence, and may name no validity date.

## The pipeline

**Pipeline** says what the open quotes and sales orders come to in the base currency, each at its
rate: the quotes sent or accepted and still valid, and the sales orders sent or accepted.

## Common problems

| Message | What to do |
|---|---|
| The quote has no validity date, or one before its date (QUOTE-03) | Enter a **Valid until** on or after the quote's date |
| Only a draft changes or is sent (QUOTE-04) | Cancel a sent quote and make a new one |
| The customer answered it already, or it expired (QUOTE-07) | Nothing to record: make a new quote if they ask again |
| Only an accepted quote or order is invoiced (QUOTE-09) | Record the customer's acceptance first |
| Nothing is left to invoice (QUOTE-10) | Every line is invoiced already: open the invoices made from it |
| Issued invoices invoice it (QUOTE-12) | Void those invoices first, or leave the quote as it is |

## Related

- [Invoices](invoices.md)
- [Contacts and items](contacts-and-items.md)
- [Document templates](document-templates.md)
