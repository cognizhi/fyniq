# Proposal

## Why

Every company files corporate income tax: the estimated chargeable income (ECI) within three months of the financial year end, then
Form C-S, C-S Lite or C. The tax agent needs the tax-adjusted profit, the exemptions and rebate worked out from the books, and a
filing pack. A GST-registered company that writes off a debt can claim GST bad-debt relief once 12 months have passed and within five
years of the supply, so the queue of eligible debts and their dates must exist. The published statutory packs change every January
and after each Budget; each organisation must see an update available with its differences.

## What Changes

- **Corporate income tax support** (FR-TAX-06): the tax-adjusted profit working paper from the ledger's profit and loss, with
  adjustments for non-deductible expenses and capital allowances entered with their reasons and evidence; the start-up and partial
  exemptions and the rebate from `sg-cit`; the form selector (C-S Lite, C-S, C) by revenue; the ECI and filing deadlines; a filing pack
  for the tax agent. Screen `filing-packs`.
- **Bad-debt relief with GST** (FR-AR-11): the queue of written-off invoices with GST, the dates (12 months from the supply or
  the debtor's insolvency; five years from the supply), and the claim recorded with the F5 period it goes in. Screen `bad-debt-relief`.
- **Published pack updates** (ENG-33): an update-available task per organisation with the diff of the tables, and an assessment task
  when a published correction touches a past period.
- **An IRAS filing port** with a local stub (FR-TAX-03): IRAS publishes CorpPass-authorised APIs for the GST return and Form C-S, whose
  payload specifications are given only to registered developers; the port, its stub and the idempotent submission record are built,
  the payload mapping is `[live-verify]`.

## Capabilities

### Modified Capabilities
- `sales/receivables` (FR-AR-11), `tax/gst-and-filings` (FR-TAX-06, FR-TAX-03), `tax/statutory-parameters` (ENG-33)

## Impact

New tables (001010 to 001029), the pack `sg-cit@2026.2` if the form thresholds need a table, a port and its stub. No change to the
posting of existing documents. The GST relief leg posts through the engine and has an oracle half and a differential scenario.
Outside needs: IRAS API access (a CorpPass-registered developer, sandbox by invitation); the tax agent's review.
