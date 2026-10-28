# Proposal

## Why

The founders can now keep the books through a month: periods, opening balances, journals, the close with its
checks, year end, the general ledger and the schedules (`p1-general-ledger-close`). What they cannot do is read the
result as statements: the P&L, the balance sheet and the cash flow. The accountant, investors and the board need
those too, exported. Reports are wave 5 of the P1–P3 plan, and their dependency, the general ledger and close, is
now built.

## What Changes

- **The owner's decisions of 28 Oct 2026 (PRD-001 rev 0.159):**
  - **re-runs (FR-RPT-08):** a report records its ledger revision and parameters. A re-run as at that revision gives
    its figures exactly, shown with today's account names and groups, and is labelled "restated presentation" when
    the chart changed since. The chart's own history comes later;
  - **the layout (FR-RPT-01), decided provisionally** [accountant to confirm], under the owner's instruction to decide
    without waiting for the accountant. It comes from each account's chart attributes:
    - the P&L runs from revenue by stream to EBITDA and net profit;
    - the balance sheet groups current and non-current items, with borrowings apart;
    - the cash flow uses the indirect method, ties to the money accounts and lists non-cash transactions apart.
- **Financial statements (FR-RPT-01, INV-10, INV-11):** the P&L, the balance sheet with its balance check and the
  movement in equity, the indirect cash flow, and the trial balance. Each statement carries its checks: the balance
  sheet balances, every account sits in one statement, the cash flow equals the money accounts' change, and equity
  reconciles. The close runs those checks too.
- **Periods and comparatives (FR-RPT-02):**
  - **periods:** a month, a quarter, YTD, a financial year, or any run of months;
  - **comparatives:** the prior period and the prior year;
  - **columns:** by month.
  - Budget and forecast comparatives come with `p1-planning`, and columns by dimension with dimensions
    (`p1-organisation-chart` 3.3).
- **Reproducible (FR-RPT-08):** every statement reads one snapshot and records its ledger revision, parameters and
  platform version. "As at" a revision reads only the lines up to it (ENG-46).
- **Exports (FR-RPT-05, FR-RPT-09, FR-RPT-10):**
  - **formats:** CSV, XLSX (typed numbers, a data tab, no text that becomes a formula) and PDF from the render
    service;
  - **large exports** run in the background behind a link valid 24 hours, checked again at each download;
  - **auditing:** every export is audited;
  - **accountant exports:** the GL, TB, journals, the fixed asset schedule, and AR and AP detail. GST detail comes with
    `p1-gst`.
- **Management reports (FR-RPT-03)** whose data exists: sales by customer and item, spend by supplier, and top
  expenses, beside the AR and AP ageing and the customer statements already built. Arriving with their changes:
  - cash and runway, with the KPI set's definitions (FR-KPI-02, `p1-dashboard-kpis`);
  - Budget vs Actual (`p1-planning`);
  - the GST return (`p1-gst`);
  - KPIs (`p1-dashboard-kpis`);
  - projects (`p1-projects`).
- **Charts (FR-VIZ-01…08):**
  - the chart component, on ECharts, as MASTER.md decides: one y-axis, colours fixed per entity, reserved status
    colours with icon and label, tooltips, legends, a table view, and PNG and data export;
  - the categorical palette validated in CI;
  - first used by the P&L's monthly trend and the top expenses.
- **Screens:** `reports-index`, `financial-statement` and `export-dialog`. Every figure opens its general ledger lines
  (the Trace panel, FR-UX-04); Basic shows plain words and Expert adds codes and variances.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`): rev 0.159's scenarios are in the main specs, converted with the PRD revision. This change
builds:
- `reporting/reports`: FR-RPT-01, 02, 03, 05, 08, 09 and 10;
- `ledger/invariants`: INV-10 and INV-11;
- `experience/visualisation`: FR-VIZ-01 to 08 and the starting palette.

## Impact

- **Code:**
  - `internal/domain/statement`, the statements from the balance service (ENG-04), which gains reads as at a revision;
  - an XLSX writer;
  - a Typst statement template;
  - the export job on the outbox;
  - the reports API.
- **Web:** the reports index, the statement page with its trace, the export dialog, and the chart component. ECharts
  is a new dependency, loaded on demand so the initial JavaScript stays within NFR-PERF-UI-03's 250 KB.
- **Migrations:** the export records and their signed links.
- **Reviewer:** the accountant, for the layout. The pages that show it get the FR-UX-41 banner through the review
  register, in their own PR.
