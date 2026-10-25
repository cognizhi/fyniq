---
key: banking.accounts
type: concept
title: Bank accounts, their data sources and freshness
summary: The money accounts in Banking, the cash position, where each account's data comes from, and what complete to and a gap mean
sources:
  - docs/user-guide/banking.md sha256:51107fd0558f6c5fbaafe3945ca677eeaa22881e9238242d230a20d36f9d4f60
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-26
aliases:
  - bank account
  - money account
  - cash position
  - wallet
  - Wise
  - Airwallex
  - bank-ingest address
  - bank reports by email
  - statement gap
  - complete to
  - read-only connection
---

# Bank accounts, their data sources and freshness

## What it is

- **Banking** lists every money account the company holds: bank current accounts, cards, wallets such as Wise or Airwallex, payment gateways and petty cash, each with its balance, how up to date its data is, and how many of its lines still wait to be categorised or matched.
- **Cash position** adds up the cash in every open money account; another currency is converted at the day's rate, and an account with no rate for the day is listed apart and left out of the total.
- A Bookkeeper or an Accountant adds money accounts and imports statements; the Owner, the Controller, the AR and AP specialists, the Payment releaser, the Auditor and the Tax agent can see them. Adding one: [[banking.add-account]].
- Connections to banks and wallets are read-only: Fyniq never moves money through them, and payments go out from the bank, as approved files or by hand.

(src: docs/user-guide/banking.md#what-it-is)

## Where its data comes from

- **Statement files**: the bank's export (CSV, XLSX, OFX, camt.053 or MT940), imported by a person; the default.
- **Bank reports by email**: the bank emails its daily report and its incoming-funds alerts to the **bank-ingest address**; only the bank's registered addresses are accepted, and only when the bank's mail passes DMARC, anything else waiting in quarantine. Each report is imported as a statement and its lines wait for **Reconcile now**. Alerts show an invoice as likely paid ([[settings.organisation]]).
- **A connector** such as Wise or Airwallex, installed and turned on under **Settings → Integrations** ([[settings.integrations]]), reads the account's transactions every hour with a read-only key.
- Setting the source and naming the bank-ingest address is for an Administrator. A source starts on a day and never changes the days before it, so switching from emailed reports to a connector leaves no gap.

(src: docs/user-guide/banking.md#where-its-data-comes-from)

## Freshness and gaps

- **Complete to** is the last day the account's statements cover with no break before it; **Last statement** is when its latest statement arrived.
- A **gap** ⚠ is a break in the balance chain, a statement whose opening balance is not the previous closing balance: import the missing statement, or resolve the gap with the reason there is none.
- An account with no statements yet says so.

(src: docs/user-guide/banking.md#freshness-and-gaps)

## Related

- Incoming-funds alerts and when they expire: [[settings.organisation]]. Connectors: [[settings.integrations]].

(src: docs/user-guide/banking.md#related)
