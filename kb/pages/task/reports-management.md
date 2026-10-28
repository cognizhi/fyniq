---
key: reports.management
type: task
title: Read the management reports
summary: Sales by customer and by item, spend by supplier and the top expenses for any run of months, net of GST, with who sees which
sources:
  - docs/user-guide/management-reports.md sha256:47d824d89dc5479942ffba79d8cc00d38f0f40d050672cd04cda133eb21ce0ca
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - management reports
  - sales by customer
  - sales by item
  - spend by supplier
  - top expenses
  - who are our biggest customers
  - what do we spend most on
---

# Read the management reports

## Find them

- In Expert mode **Reports** lists **Sales by customer and item**, **Spend by supplier** and **Top expenses**; each opens the management reports with a tab per report, beside **Who owes us** and **Whom we owe** (the customers' statements and the bills to pay).
- Choose the period: this month, last month, the year to date, this or last financial year, or any run of months. Rows show the amount in the base currency and the share of the total, largest first.

(src: docs/user-guide/management-reports.md#find-them)

## Sales by customer and by item

- **Sales by customer**: what each customer was invoiced in the months, less credit notes, net of GST. **Sales by item**: the same by item, with the quantity sold less the quantity credited; lines without an item are one row.
- Revenue spread over months counts in full in the month it is invoiced. A voided or edited invoice counts in the month it was changed, as in the profit and loss.
- Money received at the bank without an invoice is in the profit and loss, not here.

(src: docs/user-guide/management-reports.md#sales-by-customer-and-by-item)

## Spend by supplier

- What each supplier billed in the months, less their credit notes, net of the GST the company can claim; GST it cannot claim (for example when not GST-registered) is part of the cost.
- Money paid at the bank without a bill, and expense claims, are in the profit and loss and the top expenses, not here.

(src: docs/user-guide/management-reports.md#spend-by-supplier)

## Top expenses

- The ten largest expense accounts in the profit and loss for the months, and the rest as one line. Select an account to see its lines in the general ledger ([[reports.general-ledger]]).

(src: docs/user-guide/management-reports.md#top-expenses)

## Who sees them

- Anyone who can read every account sees all four. A view of the reports limited to sales (such as an AR specialist) sees the sales reports only; one limited to purchases (such as an AP specialist) the spend by supplier only.
- Every figure is read at one ledger revision, shown on the report, so the same months read again at it give the same figures ([[reports.statements]]).

(src: docs/user-guide/management-reports.md#who-sees-them)
