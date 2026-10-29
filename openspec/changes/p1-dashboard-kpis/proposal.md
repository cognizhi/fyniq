# Proposal

## Why

The workbook's first sheet was its Dashboard: 16 tiles and 4 charts for the chosen month, with a KPIs sheet of
start-up metrics and ratios beside it. Fyniq's Home today shows only what waits for the person and the set-up
checklist, because the figures needed the ledger's reports. `p1-reports` has built them: statements at a revision,
the management reports, and the `Chart` component. This change gives the founders the first-screen view
(FR-KPI-01) and the KPI set as versioned, tested definitions (FR-KPI-02, FR-KPI-03).

## What Changes

- **The owner's decision of 29 Oct 2026, taken interactively (PRD-001 rev 0.161):** until subscription data
  (FR-KPI-04, P2) and the payroll link exist, a person enters each month's paying customers, new customers,
  customers lost and headcount. They are audited; each KPI that uses them is labelled *entered*. The alternatives
  were "—" until the data exists, and counts derived from invoices.
- **Already decided:** booked MRR is the headline until subscription data exists, contracted MRR beside it once it
  does (open question 34, rev 0.25); without counts, churn, ARPA, LTV and CAC show "—" with the reason (G4-047).
- **New rounding policy `RND-KPI`** (rev 0.161): each KPI rounded once, from exact inputs, to its kind's decimals.
- **KPI definitions as versioned code** (`internal/domain/kpi`): the workbook's §11.5 set, each with its source,
  formula, date basis, currency, rounding and undefined states, read from the balance service at one revision.
- **Home as the dashboard**: the pulse card (cash, runway), the tiles whose source exists, and the charts of
  revenue, costs and profit, cash, and costs by function. Tiles of modules not built yet (budget, forecast, books
  health, unbilled time) join with their changes.
- **Reports › KPIs**: every KPI for a month with its definition, the month before, and the entered counts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None: the rev 0.161 scenarios are in the main specs `reporting/dashboards-and-kpis` and `ledger/money-and-rounding`,
converted with the PRD revision. This change builds FR-KPI-01, FR-KPI-02, FR-KPI-03 and RND-KPI
(`skip_specs: true`).

## Impact

- A migration for the entered counts (`kpi_input`, RLS, audited), the API (`getKpis`, `listKpiInputs`,
  `setKpiInput`), `internal/domain/kpi`, Home and a new KPI page, help keys and guide pages, the oracle's KPI
  expectations (an oracle session).
- Permissions (design D7): reading KPIs is the reports' view, and the board pack's qualified view reads them too;
  entering counts is "Budgets, forecasts" C.
