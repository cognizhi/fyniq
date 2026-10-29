# Design

## Context

- **Figures:** `internal/domain/financials` lays out the P&L, balance sheet and cash flow for a run of months at one
  ledger revision, and `balance.PositionAt` reads positions at a day. Receivables and payables have their open
  items. The workbook's definitions are its PRD §11.5 (`zh-sheet-accounting`, read 29 Oct 2026).
- **Screens:** `Home.html` (pulse card, needs attention, tiles, cash chart, deadlines; role variants) and
  `Kpis.html` (headline MRR, net burn, runway; every KPI with its definition and the month before).
- **Charts:** the `Chart` component (`p1-reports` 5.2) draws lines and bars with a table view.

## Decisions

### D1 Definitions as code

`internal/domain/kpi` holds one definition per KPI: key, name, version (such as `runway@1`), source (`ledger`,
`entered`, or a named operational source), formula in words, date basis, unit, decimals (`RND-KPI`) and its undefined
states with their reasons. A definition's function takes the month's inputs and returns a value or a reason. A change
of formula is a new version; the API returns the version with each value.

The set, from the workbook (§11.5), with its own words kept:

| KPI | Formula | Undefined |
|---|---|---|
| Booked MRR, ARR | subscription revenue recognised in the month (accounts whose report group is a subscription stream); × 12 | no subscription revenue account |
| Contracted MRR | active subscriptions' monthly value at month end | "no subscription data" until FR-AR-15 (P2) |
| Revenue growth | month-on-month change in total revenue | no revenue the month before |
| Gross margin, EBITDA margin, net margin | gross profit, EBITDA, net profit ÷ revenue | revenue 0 |
| Spend by function | S&M, R&D and G&A totals from each account's function | — |
| Net burn | −(operating + investing cash flow) for the month | — |
| Runway | month-end cash ÷ 3-month average net burn | "Not burning" when the average ≤ 0 |
| ARPA | booked MRR ÷ paying customers | no counts entered, or 0 customers |
| Churn | customers lost ÷ customers at the start of the month | no counts, or 0 at the start |
| CAC | S&M spend ÷ new customers | no counts, or 0 new |
| LTV, LTV:CAC, CAC payback | ARPA × gross margin ÷ churn; LTV ÷ CAC; CAC ÷ (ARPA × gross margin) | any input undefined, churn 0 |
| Burn multiple | net burn ÷ net new ARR | blank when not burning, or ARR not grown |
| Current, quick ratio | current assets ÷ current liabilities; (cash + receivables) ÷ current liabilities | current liabilities 0 |
| Working capital, debt/equity | current assets − current liabilities; total liabilities ÷ equity | equity ≤ 0 |
| DSO, DPO | receivables ÷ (3-month average revenue ÷ 30); payables and accruals ÷ (3-month average costs ÷ 30) | average 0 |
| Headcount, revenue per employee | entered headcount; revenue × 12 ÷ headcount | no headcount entered, or 0 |

Three-month averages use the months of the selected financial year that exist (fewer in its first two months), as
the workbook did. Months after the books' last period are not read.

### D2 One reading

A month's KPIs read the P&L, balance sheet and cash flow for the months they need through `financials` at one ledger
revision, which the answer returns, so asking again as at it gives the same KPIs (FR-RPT-08).

### D3 Entered counts (rev 0.161)

`kpi_input` holds, per month, paying customers, new customers, customers lost and headcount (whole numbers ≥ 0), with
who entered them and when. Every change is audited. A month's row is replaced, never deleted; a closed month's counts
can still be corrected, since they are not ledger figures, and each correction is audited. Each KPI that uses them is
labelled *entered*, with who and when.

### D4 Rounding

`RND-KPI`: amounts at the base currency's minor units; percentages and margins to 1 decimal; ratios, multiples and
months to 1 decimal; counts whole; each rounded once from exact inputs, half away from zero. Nothing undefined is ever
0.

### D5 Home as the dashboard

- **Pulse card:** cash at the end of today's month (money accounts), and runway or "Not burning".
- **Tiles (FR-KPI-01)**, with the source they need now: cash, burn, runway, revenue this month, gross margin, net
  profit for the year to date, owed to us, owed by us, pipeline (open quotes and sales orders), recurring due (the
  next 30 days' recurring invoices and bills), and unrealised FX on open items. Basic shows cash, burn, owed to us and
  we owe; Expert shows the rest.
- **Charts (FR-KPI-01):** revenue, costs and profit by month; cash by month; costs by function. The 36-month cash
  forecast joins with `p1-planning`.
- **Joining later, with their changes:** revenue and cost against budget and the forecast cash-out date
  (`p1-planning`), books health (`p1-checks`), unbilled time (`p1-projects`), GST due and deadlines (`p1-gst`).
  A tile whose module is built but whose data is missing shows "—" with the reason (FR-KPI-03).
- Each tile shows its definition on hover and opens what is behind it (FR-UX-04).

### D6 The KPI page

`/reports/kpis`: the month's KPIs with the month before, each with its definition, source, version and undefined
reason; both MRR measures, explained; and the entered counts, which whoever may enter them changes there.

### D7 Permissions

- **Reading** (`getKpis`): Reports, dashboards V, unqualified, and also the board pack's qualified view (the board
  viewer sees KPIs; Home's role variant), through the policy's `Qualified` list (`p1-reports` D9).
- **Entering counts** (`setKpiInput`): "Budgets, forecasts" C, unqualified (Owner, Controller, Accountant); reading
  them (`listKpiInputs`) the same view as the KPIs.

### D8 The oracle

The oracle computes the KPIs of the sample company independently from §11.5's words, and the differential test
compares them to the cent and the decimal (QA-ENG-02). That is written in an oracle session, not by whoever wrote the
Go definitions.

## Risks

- **Entered counts go stale.** The KPI page shows when each month's counts were entered and by whom; a month without
  them shows "—" with the reason.
- **Which accounts are subscription revenue.** Booked MRR reads the accounts whose report group names a subscription
  stream in the chart; the KPI page says which accounts it read. [accountant to confirm]
