---
key: payroll.salary-payments
type: task
title: Pay salaries from a pay run
summary: Prepare a salary batch, have another person approve it, release the lines free to go, and see each payment paid from the bank feed or a person's evidence
sources:
  - docs/user-guide/payroll-salary-payments.md sha256:0826f0cdcb32f31a3e5c107f80da3c9e0fefe2c6c9b946da985bb8f178b325e8
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - salary batch
  - salary payments
  - pay salaries
  - release salaries
  - payroll payment
  - salary lines
  - hold for tax clearance
  - paid means paid
  - unknown outcome
---

# Pay salaries from a pay run

## What it is

- When a pay run approved in Nemura has been posted here, Nemura sends one salary line for each person; they arrive as a salary batch, such as SB-0003.
- It is prepared, approved by another person and released like a payment batch ([[purchases.payment-batches]]).
- A salary is never made a contact or a bill, and is never shown to someone with no need to see it.

(src: docs/user-guide/payroll-salary-payments.md#what-it-is)

## Before you start

- Payroll lives in Nemura and the link in **Settings › People and HR** is active.
- The pay run has been posted here: lines are taken only for the version posted last.
- The bank account it is paid from is set up in **Banking**, in SGD ([[banking.add-account]]).
- How the month's payslips, key employment terms, CPF, SDL and SHG, and where they apply the Auto-Inclusion Scheme and IR21 are met is recorded as evidence; while one is missing, a salary batch cannot be asked for approval.

(src: docs/user-guide/payroll-salary-payments.md#before-you-start)

## Prepare a salary batch

- **Payroll › Salary payments**, open the batch and **Prepare the batch**: the bank account, the payment date, GIRO or FAST.
- A batch prepared again needs its approval again.
- **Cancel the batch** is for a batch with nothing released.

(src: docs/user-guide/payroll-salary-payments.md#prepare-a-salary-batch)

## Approve a salary batch

- **Ask for approval** and confirm it's you; another person who holds the approval right on pay runs approves it from **Inbox › To approve**, signing for the total and the lines, never a name or an account ([[approvals.decide]]).
- Neither whoever prepared the pay run in Nemura nor whoever approved it there approves or releases its salary batch; whoever asked for the approval never approves it.

(src: docs/user-guide/payroll-salary-payments.md#approve-a-salary-batch)

## Release the free lines

- The Payment releaser selects **Release** and confirms it's you; the lines free to go are written to the payment file with an instruction ID each, to pay in the bank.
- A person held for tax clearance, one whose bank details changed after approval and one with an unknown outcome wait while the others are paid, each in a later release.
- Fyniq asks Nemura whether each person is held and waits up to five seconds; with no answer the line waits.
- A changed line makes the batch show **Needs approving again**.
- A release is never "paid"; the file holds names and account numbers, so key it into the bank or upload it, then delete it.

(src: docs/user-guide/payroll-salary-payments.md#release-the-free-lines)

## What became of a payment

- A line is **paid** only when the bank feed shows its debit, matched to the lines it paid, or a person records the outcome with a file of evidence; a posted journal or a payment file is never payment.
- Someone who did not prepare or approve the pay run in Nemura records it as sent, accepted, paid, rejected, returned or unknown, with the day and the evidence.
- An unknown outcome blocks paying that person again in the run until it is resolved as paid, rejected or returned.
- Each outcome goes back to Nemura.

(src: docs/user-guide/payroll-salary-payments.md#what-became-of-a-payment)

## Who sees the lines

- The Owner and the Payroll administrator see each person, the account masked to its last four digits and the amount.
- The Payment releaser, the Controller, the Accountant and the Auditor see the batch's total, the number of people and its state, and no line.
- Names and accounts are in no notification, audit trail entry or log.

(src: docs/user-guide/payroll-salary-payments.md#who-sees-the-lines)
