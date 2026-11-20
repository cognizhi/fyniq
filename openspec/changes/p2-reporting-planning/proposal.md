# Proposal

## Why

P1 gave Fyniq the statements, the KPIs, the budget and forecast, the equity register and the Board viewer's landing page. After
go-live the founders and the accountant need the rest of the management layer: a report they can shape themselves and send on
a schedule, a monthly board pack and an investor update built from the same figures, dashboards a role can arrange, the
deferred-revenue and prepayment roll-forward the auditor asks for, the asset and capital-allowance schedules, a forecast that
keeps itself current and says how wrong it was, and the investor-facing and auditor-facing parts of the equity register.

## What Changes

- **Report builder, saved and scheduled** (FR-RPT-04, FR-RPT-07): a saved definition (rows of accounts, groups and calculated rows;
  columns of periods and comparatives; filters), run through the same balance service as the statements, shared with roles
  and scheduled by email, with permissions checked again at send time. Screen `report-builder`.
- **Board pack** (FR-RPT-06): the monthly pack assembled from the statements and KPIs, with commentary a person writes or edits,
  published as an immutable version and read by the Board viewer after signing in. Screen `board-pack`.
- **Investor updates, access and obligations** (FR-EQ-23, FR-EQ-24, FR-EQ-28): an update built from a published pack, sent to a
  distribution list as a notice that points to the signed-in page, with read status and an archive; the Board / investor viewer sees
  the updates, the pack and their own holdings, and the full cap table only when an Owner grants it; investors' information rights
  become calendar tasks. Screens `investor-updates` and `board-portal`.
- **Dashboards** (FR-VIZ-09, FR-KPI-05, FR-KPI-04, FR-KPI-06, FR-KPI-07): tiles a person adds, removes, resizes and saves per role or
  for themselves, each with a "last updated" time and a drill-down; customer counts from a count source behind a port; anomaly alerts as
  Inbox items; industry bands from benchmark packs. Screen `dashboards`.
- **Deferral roll-forward and asset reports** (FR-RPT-13, FR-FA-04): the roll-forward of deferred revenue and prepayments tying to
  2400 and 1210, and the asset schedule and the capital allowance schedule from an effective-dated pack. Screen `management-report`.
- **Rolling forecast and funding plan** (FR-PLN-04, FR-PLN-07): forecast months replaced by actuals as periods close, the weekly
  projections kept and compared with actual cash, and a funding plan that starts from the cap table.
- **The Auditor's engagement** (FR-UX-42): requests answered with evidence, and samples drawn from the ledger by the auditor's rule.
- **Exit waterfall, allocation rules and shares for services** (FR-EQ-29, FR-DIM-03, FR-EQ-13): the sandboxed waterfall for the
  structures a lawyer has confirmed, allocation rules posted as approved journals, and shares for services expensed over the service
  period. The last two post through the engine, with the oracle's half and a differential scenario. Screen `exit-waterfall`.

## Capabilities

### Modified Capabilities
- `reporting/reports` (FR-RPT-04, 06, 07, 13), `reporting/dashboards-and-kpis` (FR-KPI-04 to 07), `experience/visualisation` (FR-VIZ-09),
  `experience/navigation` (FR-UX-42), `planning/budgets-and-forecasts` (FR-PLN-04, 07), `equity/cap-table-and-investors` (FR-EQ-23,
  24, 28, 29), `equity/founders-money` (FR-EQ-13), `ledger/chart-of-accounts` (FR-DIM-03), `ledger/fixed-assets` (FR-FA-04)

## Impact

New tables in 001100 to 001149. No change to how existing documents post; the two new posting sources (allocation, shares for
services) have the oracle's half and differential scenarios. New operations, each with an authorisation-matrix row. No new
scope for the AI or MCP surface: the commentary draft (`rpt.draft_commentary`) is a catalogue task type built in
`p2-ai-expansion`, and the pack works without it. Outside needs: none to build; the benchmark data and a lawyer's confirmation of each
waterfall structure are the owner's to supply (`[verify licences]`, `[accountant to confirm]`).
