---
key: settings.organisation
type: concept
title: Organisation settings and their recommended values
summary: The company-wide settings (features, payment terms, settlement tolerances, capitalisation threshold, data region, retention) and the value each ships with
sources:
  - docs/user-guide/organisation-settings.md sha256:f374e8f2e9205e304168f6468e9098e08e4802c0b2b07d7f9c9fad2f38e41320
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - feature switches
  - default settings
  - tolerance
  - retention period
  - data residency
---

# Organisation settings and their recommended values

## What they are

- **Settings → Organisation** (Expert mode) holds the profile documents print and the choices that apply to the whole company.
- Each setting starts at its recommended value, with a reason and a link to the guide.
- It shows **Recommended** while it holds that value, **Changed from recommended** once it differs.
- The Owner, the Administrator and the Controller change them; others in the company can see them. How: [[settings.change]].

(src: docs/user-guide/organisation-settings.md#what-it-is)

## Features

- Switching a feature off hides it from menus and pages.
- Recommended: **Multi-currency** on; **Payroll**, **Projects and time** and **Portals for customers and investors** off.
- The **AI assistant** is off until an Owner turns it on with a second approver.

(src: docs/user-guide/organisation-settings.md#features)

## Payment terms

- Invoices and bills with no terms of their own fall due 30 days after their date.
- A contact's own payment terms always win.

(src: docs/user-guide/organisation-settings.md#payment-terms)

## Settlement tolerances

- A small difference between a payment and its document clears to **FX and rounding** (7030) instead of leaving it part-paid.
- Base currency: up to S$1.00. Foreign currency: the greater of S$1.00 and 3% of the document.
- Never more than S$50 on one document, or S$200 in a month in all.
- Exchange-rate gains and losses come first and are never limited by a tolerance.
- An overpayment above S$1.00 stays with the customer as a credit; anything above tolerance needs a reason-coded adjustment.

(src: docs/user-guide/organisation-settings.md#settlement-tolerances)

## Capitalisation threshold

- A purchase of S$1,000 or more is offered as a fixed asset; a smaller one is an expense.
- The company's accountant confirms it.

(src: docs/user-guide/organisation-settings.md#capitalisation-threshold)

## Data region and retention

- Data region: Google Cloud's Singapore region, set by the platform and read-only.
- Records (ledger, documents, files, audit trail) are kept 7 years after the end of their financial year; this only lengthens.
- AI conversations are kept 365 days; proposals stay with the audit trail.

(src: docs/user-guide/organisation-settings.md#data-region-and-retention)

## Related

- Setting up the accounts themselves: [[accounting.chart]]. Approving a change: [[approvals.decide]].

(src: docs/user-guide/organisation-settings.md#related)
