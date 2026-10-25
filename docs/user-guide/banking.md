---
key: banking.accounts
title: Bank accounts
mode: basic
roles: [bookkeeper, accountant, controller, owner, administrator, ar_specialist, ap_specialist, payment_releaser, auditor, tax_agent]
requirements: [FR-BNK-01, FR-BNK-13, FR-BNK-15, FR-BNK-16, FR-BNK-17, FR-BNK-18, FR-INT-22]
status: draft            # written with the banking screens (P1, p1-bank-import-reconcile task 10.1); reviewed with the accountant
---

# Bank accounts

## What it is

**Banking** shows every money account the company holds: bank current accounts, cards, wallets such as
Wise or Airwallex, payment gateways and petty cash. For each one it shows its balance, how up to date its
data is, and how many of its bank lines still wait to be categorised or matched.

- **Cash position** at the top adds up the cash in every open money account. An account in another
  currency is converted at the day's rate; one with no rate for the day is listed apart and left out of
  the total.
- A Bookkeeper or an Accountant adds money accounts and imports statements. The Owner, the Controller,
  the AR and AP specialists, the Payment releaser, the Auditor and the Tax agent can see them.
- Connections to banks and wallets are read-only: Fyniq never moves money through them. Payments go out
  from your bank, as approved files or by hand.

## Add a money account

Select **Add account** and follow the three steps.

1. **The account**: its kind (bank, card, wallet, payment gateway or cash), its name, the bank or
   provider, and the account in the chart it posts to. Only an account marked as a money account in the
   chart can be chosen; its currency is the money account's. Enter the account number, or the provider's
   balance ID for Wise or Airwallex: Fyniq shows only its last four characters afterwards.
2. **Where its data comes from**: statement files you import, the bank's reports by email, or a
   connector such as Wise or Airwallex. See [Where its data comes from](#where-its-data-comes-from).
3. **Check**: what happens next, such as waiting for the first emailed report.

## Where its data comes from

- **Statement files**: you import the bank's export (CSV, XLSX, OFX, camt.053 or MT940). This is
  the default, and needs nothing more.
- **Bank reports by email**: the bank emails its daily report, and its incoming-funds alerts, to your
  **bank-ingest address**. Only the bank's addresses you register are accepted, and only when the
  bank's mail passes DMARC; anything else waits in quarantine for someone to decide. The worker imports
  each report as a statement, and its lines wait for **Reconcile now**.
- **A connector**: an organisation connector such as Wise or Airwallex, installed and turned on under
  **Settings → Integrations**, reads the account's transactions every hour with a read-only key.

Setting where the data comes from, and naming the bank-ingest address, is for an Administrator. A
source starts on a day and is never changed for the days before it, so switching from emailed reports to
a connector leaves no gap.

## Freshness and gaps

- **Complete to** is the last day the account's statements cover with no break before it.
- **Last statement** is when its latest statement arrived.
- A **gap** ⚠ is a break in the balance chain: a statement whose opening balance is not the previous
  closing balance. Import the missing statement, or resolve the gap with the reason there is none.
- An account with no statements yet says so.

## Related

- Incoming-funds alerts and when they expire: [Organisation settings](organisation-settings.md).
- Connectors: [Integrations](integrations.md).
