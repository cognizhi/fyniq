---
key: sales.statements
title: Statements and reminders
mode: basic
roles: [bookkeeper, ar_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AR-08, FR-AR-18, FR-INT-21]
status: draft            # written with the sales screens (P1, p1-sales task 8.1); reviewed with the accountant
---

# Statements and reminders

## What it is

**Sales → Statements and reminders** is where the company chases what customers owe: the reminders
due today, a customer's statement of account, and when each customer is reminded.

## Reminders due

An issued invoice with something still open is due a reminder on each day of its customer's schedule,
counted from its due date. A sent quote or sales order with no answer is due one 3 days before its
validity date. Only the latest day reached is listed; sending or skipping it settles the earlier ones.

Until bank import records payments by itself, no reminder goes by itself: each waits here to be sent
or skipped, so a customer who has paid is never chased for a payment not yet recorded. Record the
payment first, and its reminder goes from the list.

- **Send** emails the reminder from the organisation's reminders sender, with the PDF of the invoice
  and a secure link. Once the invoice is overdue it is worded as a payment reminder; before, as the
  invoice's own email.
- **Skip** sends nothing, with a reason if you give one.

An AR specialist or an Accountant sends and skips reminders. A reminder is sent or skipped once.

## Statements of account

Choose the **Customer**, the period (**From** and **To**, ending today at the latest) and, if the
customer trades in more than one, the **Currency**, then select **Show it**. The statement shows:

- the balance brought forward from the day before the period;
- each invoice, credit note, payment received, refund of a credit, write-off and void in the period,
  with a running balance;
- the balance due at the end of the period, and its ageing: not yet due, 1–30, 31–60, 61–90 and over
  90 days, and unapplied credits, which add up to it.

It says whether it adds up. One that does not is never made: ask the accountant to look at the
customer's documents.

**Make and email** keeps the statement as it says, numbered from the statement sequence, and emails it
with its PDF from the invoices sender. A statement made never changes and is never voided: for a
correction, make a new one. A Bookkeeper makes statements; an AR specialist or an Accountant emails
them.

## Reminder schedules

The **Standard** schedule reminds 3 days before the due date, on it, and 7 and 30 days after. A
**customer group** names its own: at most 6 days, from 30 before to 365 after the due date, or none for
no reminders. Select **New group**, name it and list its days, such as -3, 0, 7, 30. A customer joins a
group under **Customer group** on its contact; one in no group follows the standard schedule. An
inactive group keeps its customers and is offered to no new one.

## Common problems

| Message | What to do |
|---|---|
| That reminder is not due (REMINDER-02) | It was sent or skipped already, or a payment settled the invoice: refresh the list |
| The reminder schedule is not valid (REMINDER-01) | List at most 6 days from -30 to 365, earliest first, each once |
| The customer has no billing email (SALESDOC-04) | Add the customer's billing email on its contact |
| The statement's period is not valid (STATEMENT-01) | End the period today or earlier, after it starts, within two years |
| The statement does not add up (STATEMENT-03) | Ask the accountant to look at the customer's documents |
| The customer has nothing in that currency (STATEMENT-04) | Choose a currency the customer trades in, or another period |

## Related

- [Invoices](invoices.md)
- [Quotes and orders](quotes.md)
- [Contacts and items](contacts-and-items.md)
