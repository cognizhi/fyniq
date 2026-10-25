---
key: sales.statements
type: task
title: Statements and reminders
summary: Sending or skipping the reminders due to customers, a customer's statement of account that adds up, and the reminder schedules of customer groups
sources:
  - docs/user-guide/statements.md sha256:d66a2dad4743637f60438a1c4649eb3669ea9c9a1c8b5e17b09b09c52a597e11
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-25
aliases:
  - payment reminder
  - chase a customer
  - overdue reminder
  - dunning
  - reminder schedule
  - customer group
  - skip a reminder
  - statement of account
  - customer statement
  - account statement
  - balance brought forward
  - ageing
  - what a customer owes
---

# Statements and reminders

## What it is

- **Sales → Statements and reminders** is where customers are chased: the reminders due today, a customer's statement of account, and when each customer is reminded ([[sales.invoices]]).

(src: docs/user-guide/statements.md#what-it-is)

## Reminders due

- An issued invoice with something open is due a reminder on each day of its customer's schedule from its due date; a sent quote or sales order with no answer, 3 days before its validity date ([[sales.quotes]]). Only the latest day reached is listed.
- Until bank import records payments, none goes by itself: each waits to be sent or skipped, so a customer who paid is never chased; record the payment first and its reminder goes.
- **Send** emails it from the reminders sender with the invoice's PDF and a secure link, worded as a payment reminder once overdue; **Skip** sends nothing, with a reason if given. An AR specialist or an Accountant decides each, once.

(src: docs/user-guide/statements.md#reminders-due)

## Statements of account

- Choose the **Customer**, **From** and **To** (ending today at the latest) and the **Currency** if it trades in several, then **Show it**: the balance brought forward, each invoice, credit note, payment, refund, write-off and void with a running balance, and the balance due with its ageing (not yet due, 1–30, 31–60, 61–90 and over 90 days, and unapplied credits), which adds up to it.
- One that does not add up is never made. **Make and email** keeps it, numbered, and emails its PDF from the invoices sender; it never changes and is never voided. A Bookkeeper makes statements; an AR specialist or an Accountant emails them.

(src: docs/user-guide/statements.md#statements-of-account)

## Reminder schedules

- **Standard**: 3 days before the due date, on it, and 7 and 30 days after. A **customer group** names its own, at most 6 days from 30 before to 365 after, or none; a customer joins one under **Customer group** on its contact ([[contacts.manage]]), and an inactive group keeps its customers.

(src: docs/user-guide/statements.md#reminder-schedules)

## Common problems

- REMINDER-02: sent, skipped or settled already. REMINDER-01: at most 6 days from -30 to 365, earliest first. SALESDOC-04: add the billing email.
- STATEMENT-01: a period ending today or earlier, within two years. STATEMENT-03: ask the accountant. STATEMENT-04: a currency the customer trades in.

(src: docs/user-guide/statements.md#common-problems)
