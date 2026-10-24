---
key: sales.quotes
type: task
title: Quotes and orders
summary: Preparing, sending and emailing a quote or sales order; recording the customer's answer; invoicing an accepted quote; cancelling; the pipeline
sources:
  - docs/user-guide/quotes.md sha256:876fca896e43b278c8cc1acd694bb90862c3faa90890713ca65ef29b6011c4d8
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-25
aliases:
  - quote
  - quotation
  - estimate
  - proposal
  - sales order
  - customer order
  - send a quote
  - email a quote
  - quote accepted
  - quote declined
  - quote expired
  - validity date
  - convert a quote to an invoice
  - invoice a quote
  - cancel a quote
  - sales pipeline
  - open quotes
---

# Quotes and orders

## What it is

- **Sales → Quotes and orders** keeps the quotes the company offers and the orders customers place; neither posts anything, and an accepted one becomes an invoice ([[sales.invoices]]).
- A quote is **Draft**, **Sent**, **Accepted** or **Declined**, **Expired** once its validity date passes unanswered, **Invoiced** once issued invoices take every line, or **Cancelled**.
- A Bookkeeper, an AR specialist or an Accountant prepares quotes and invoices accepted ones; an AR specialist or an Accountant sends, emails, records answers to and cancels them.

(src: docs/user-guide/quotes.md#what-it-is)

## Prepare a quote

- **New quote**: choose the **Customer**, enter their reference, the **Quote date** and **Valid until** (on or after its date); each line is entered as for an invoice. **Save as a draft** shows the totals and GST.

(src: docs/user-guide/quotes.md#prepare-a-quote)

## Send it

- **Send** numbers it from the quote sequence and counts it in the pipeline; **Email** sends it with its PDF and a secure link where the customer accepts or declines online.
- A reminder of a quote about to expire, 3 days before its validity date, waits in the Inbox to be sent or skipped.

(src: docs/user-guide/quotes.md#send-a-quote)

## The customer's answer

- Answered through the secure link, the customer records it, with their name and comment kept as evidence; answered by email or phone, **Record accepted** or **Record declined** with the day, who answered and a note.

(src: docs/user-guide/quotes.md#the-customers-answer)

## Invoice it

- **Make the invoice** on an accepted quote makes a draft invoice of everything not invoiced yet; **Invoicing** shows each line's quoted, invoiced, drafted and left.

(src: docs/user-guide/quotes.md#invoice-an-accepted-quote)

## Cancel it

- **Cancel**, with a reason, withdraws a sent quote or order: it stays in the records, marked cancelled, and leaves the pipeline. One that issued invoices take is not cancelled.

(src: docs/user-guide/quotes.md#cancel-a-quote)

## Sales orders

- **New sales order** records a customer's order, entered and sent as a quote, numbered from the sales order sequence, with no validity date needed.

(src: docs/user-guide/quotes.md#sales-orders)

## The pipeline

- **Pipeline** is what the open quotes (sent or accepted and still valid) and sales orders (sent or accepted) come to in the base currency, each at its rate.

(src: docs/user-guide/quotes.md#the-pipeline)

## Common problems

- QUOTE-03: a **Valid until** on or after the quote's date. QUOTE-04: cancel a sent quote and make a new one. QUOTE-07: it was answered or expired already.
- QUOTE-09: record the acceptance first. QUOTE-10: every line is invoiced already. QUOTE-12: void those invoices first.

(src: docs/user-guide/quotes.md#common-problems)
