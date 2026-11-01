---
key: banking.add-account
type: task
title: Add a money account and choose where its data comes from
summary: Add a bank, card, wallet, gateway or cash account in three steps, and set its statements to arrive as files, by email or through a connector
sources:
  - docs/user-guide/banking.md sha256:930af81aef70c44afbf4c03e78e113d8e6cfcf0e531ecd9c7c2f14a358ed5332
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - add a bank account
  - new money account
  - connect a bank
  - add Wise
  - add Airwallex
  - DBS daily report
---

# Add a money account and choose where its data comes from

## Steps

- In **Banking**, select **Add account** and follow the three steps.
- **The account**: its kind (bank, card, wallet, payment gateway or cash), its name, the bank or provider, and the chart account it posts to; only an account marked as a money account in the chart can be chosen, and its currency is the money account's. Enter the account number, or the provider's balance ID for Wise or Airwallex; only its last four characters show afterwards.
- **Where its data comes from**: statement files, the bank's reports by email, or a connector such as Wise or Airwallex ([[banking.accounts]]).
- **Check**: what happens next, such as waiting for the first emailed report.

(src: docs/user-guide/banking.md#add-a-money-account)

## Who sets the data source

- Statement files need nothing more; setting bank reports by email or a connector, and naming the bank-ingest address, is for an Administrator.
- A source starts on a day and never changes the days before it.

(src: docs/user-guide/banking.md#where-its-data-comes-from)
