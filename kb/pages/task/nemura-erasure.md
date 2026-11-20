---
key: nemura.erasure
type: task
title: Erase a former employee's data
summary: How an Owner or HR administrator asks for a former employee's data to be erased after its retention, who approves, and what stays
sources:
  - docs/user-guide/nemura/erasure.md sha256:763ccd161cbbf21d275b6b56724f36182fb2c253486aac63e708847400e88b21
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - erasure
  - delete a former employee
  - retention
---

# Erase a former employee's data

## What it is

- An Owner or an HR administrator asks, and a second Owner or HR administrator approves, for a former employee's personal data to be erased once the retention periods have run: one year after leaving for the employment records and, for someone paid through Nemura, five years from the relevant Year of Assessment for the payroll records (the accountant confirms the reach for IR8A and CPF records). (src: docs/user-guide/nemura/erasure.md#what-it-is)

## Steps

1. Open **Erasures**, enter the person's ID and why, and select **Ask for the erasure** with a passkey.
2. A second Owner or HR administrator approves with a passkey; Nemura then erases and keeps a signed record outside its database.

(src: docs/user-guide/nemura/erasure.md#ask-for-an-erasure)

## What is erased, and what stays

- Emptied: names, work and personal email, phone, address, emergency contacts, NRIC or FIN, salary account and reason for leaving; sign-in switched off; files deleted; Fyniq told to clear the name and email it held. The itemised figures of new payslips are sealed under the person's own key, which erasing destroys. Issued letters, older payslip figures, pay run lines and leave and sick notes stay, tied to the person's ID with no name, until the people team decides how they are shredded. After a restore the signed record is applied again. (src: docs/user-guide/nemura/erasure.md#what-is-erased-and-what-stays)

## When it looks wrong

- Refused: the person has not left, the periods have not run (the page names the date), a legal hold keeps the records, an access request is unanswered, or you asked for it yourself and a second person must approve. (src: docs/user-guide/nemura/erasure.md#common-problems)
