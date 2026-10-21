---
key: settings.organisation
type: concept
title: Organisation settings and their recommended values
summary: Company-wide settings (features, payment terms, tolerances, capitalisation and self-approval thresholds, data region, retention) and their recommended values
sources:
  - docs/user-guide/organisation-settings.md sha256:c8a306cd86f4766832f990f00055d7aef53deba9635a105aabb8de965bc13615
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
  - self-approval threshold
  - self-approved
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
- The company's accountant confirms it by approving the capitalisation threshold policy under **Settings → Accounting framework and policies** ([[settings.accounting]]).

(src: docs/user-guide/organisation-settings.md#capitalisation-threshold)

## Self-approval threshold

- In Founder mode a founder posts a journal of up to S$2,000 alone; it is flagged as self-approved and listed in the weekly review, where another founder reviews it.
- Above the threshold, posting a journal needs another founder's approval, and the one who prepared it never approves it ([[approvals.decide]]).
- Changing the threshold needs a second approver.

(src: docs/user-guide/organisation-settings.md#self-approval-threshold)

## Data region and retention

- Data region: Google Cloud's Singapore region, set by the platform and read-only.
- Records (ledger, documents, files, audit trail) are kept 7 years after the end of their financial year; this only lengthens.
- AI conversations are kept 365 days; proposals stay with the audit trail.

(src: docs/user-guide/organisation-settings.md#data-region-and-retention)

## Related

- Setting up the accounts themselves: [[accounting.chart]]. Approving a change: [[approvals.decide]]. The accounting framework and policies: [[settings.accounting]].

(src: docs/user-guide/organisation-settings.md#related)
