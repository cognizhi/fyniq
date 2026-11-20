---
key: banking.accounts
type: concept
title: Bank accounts, their data sources and freshness
summary: Money accounts and the cash position, where their data comes from, an account's statements and lines, duplicates, alerts and gaps
sources:
  - docs/user-guide/banking.md sha256:bdc2177c8bb0f0a6ea043d0475036f8601fa128c0eeff0245f18f79ff8973159
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-06
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
  - payment file
  - read-only connection
  - possible duplicate
  - statement lines
  - transfer in transit
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
- **Bank reports by email**: the bank emails its daily report and its incoming-funds alerts to the **bank-ingest address**; only the bank's registered addresses are accepted, and only when the bank's mail passes DMARC, anything else waiting in quarantine. Each report is imported as a statement and its lines wait for **Reconcile now**; a report in the **MT942** intraday format makes only pending "likely paid" signals for its credits, never statement lines, and the daily MT940 closes them. Alerts show an invoice as likely paid ([[settings.organisation]]).
- **A connector** such as Wise or Airwallex, installed and turned on under **Settings → Integrations** ([[settings.integrations]]), reads the account's transactions every hour with a read-only key.
- **A bank's SFTP folder**: the **Bank statement files over SFTP** connector reads the statement files (MT940, camt.053 or OFX) the bank leaves in its SFTP folder, in file-name order, every hour; the host key is pinned by its fingerprint, the private key is the connector's secret, and the money account's identifier is the account number the statements name. It only reads; a PGP-wrapped file stops the sync with a reason. Unverified against a real bank until onboarding.
- Setting the source and naming the bank-ingest address is for an Administrator. A source starts on a day and never changes the days before it, so switching from emailed reports to a connector leaves no gap.

(src: docs/user-guide/banking.md#where-its-data-comes-from)

## The file its payment batches give

- A payment batch paying from a bank or wallet account gives a file when released; the account's **Payment file** says which, on its card under **Banking** and on step 2 of **Add account**.
- **The payment list** (the default) is a CSV keyed into the bank's bulk payment screen, for any bank; **Wise's batch payment file** is a CSV in Wise's batch template, uploaded to Wise, only for an account at Wise; **DBS IDEAL's UFF** is uploaded to DBS IDEAL to pay by FAST or GIRO, only for an account at DBS.
- A bank's own file can be chosen only once Fyniq writes it: until then it is listed with the reason and the account keeps the payment list. Airwallex has no file of its own yet, so an Airwallex account gives the payment list.
- A Bookkeeper or an Accountant selects **Payment file** on the card, chooses a file and selects **Save the payment file**; the change is audited with the old and new file. A batch already released keeps its file, so a change never alters a file you may have uploaded; others see the file and cannot change it.
- A change of the account's bank to one that does not take its file is refused, saying which file to choose first.

(src: docs/user-guide/banking.md#the-file-its-payment-batches-give)

## An account's statements and lines

- An account's page lists its **statements**, each with its days, opening and closing balances, lines and how it came (a file, an emailed report or a connector): **Continues** when its opening is the previous closing, **Gap before it** ⚠ when not.
- Its **lines** show the booking date, description, counterparty, amount and status: **Waiting**, **Reconciled**, **In suspense** ⚠, **Possible duplicate** ⚠ or **Excluded**; descriptions are shown as the bank sent them, as text.
- A **possible duplicate** looks like a line already held without the bank's reference or a running balance to prove it: **Compare** shows both side by side, then **Keep it** as a line of its own or **Exclude it** with the reason; neither posts anything.
- **Incoming-funds alerts** are listed apart, as pending, never as lines: each names the invoice it likely pays, and **Dismiss** removes a wrong one with a reason.
- **Transfers in transit** are transfers recorded with the account whose other side's line has not arrived yet.
- Bringing statements in: [[banking.import]]. Working the waiting lines: [[banking.categorise]]. Proving a month: [[banking.reconcile]].

(src: docs/user-guide/banking.md#an-accounts-statements-and-lines)

## Freshness and gaps

- **Complete to** is the last day the account's statements cover with no break before it; **Last statement** is when its latest statement arrived.
- A **gap** ⚠ is a break in the balance chain, a statement whose opening balance is not the previous closing balance: import the missing statement, or resolve the gap with the reason there is none.
- An account with no statements yet says so.

(src: docs/user-guide/banking.md#freshness-and-gaps)

## Related

- Incoming-funds alerts and when they expire: [[settings.organisation]]. Connectors: [[settings.integrations]].

(src: docs/user-guide/banking.md#related)
