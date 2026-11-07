# Design

## Context

- **The workbook's model.** The workbook's Forecast, Feasibility and Funding sheets carry an independent reference
  model in zh-sheet-accounting: `tests/ref_planning.py`, written from that project's PRD and user guide, never from
  the sheet's formulas. It works in binary floating point, with one value per statutory parameter. The owner chose on
  8 Nov 2026 (PRD-001 rev 0.214) to keep that model as it is, worked in exact decimals under `RND-PLAN`, with the
  statutory figures from packs.
- **What exists today:**
  - **Statutory packs:** `sg-gst` and `sg-deadlines` (`internal/domain/pack`); no CPF, SDL or corporate tax pack yet.
    `p1-statutory-packs` task 4.1 lists `sg-cpf` and `sg-sdl`; this change writes the part the forecast needs.
  - **The KPIs (`internal/domain/kpi`)** give each month's net burn, −(operating + investing cash flow), from the
    statements, and runway (C.1 #36, a fixed 6 months).
  - **The sub-ledgers** (`internal/domain/receivables`, `internal/domain/payables`) give open items, their due dates
    and how they were settled. Bills carry a planned payment date; recurring invoices and recurring bills have
    schedules (`internal/domain/journal`); payment batches are approved before they are released.
  - **The fixed-asset register** depreciates by the scheduler, from a schedule per asset.
  - **Read-only POSTs:** a collection action that saves nothing already has a pattern, `POST /v1/bank-rules:back-test`.
  - **Documents:** a document that posts nothing, is versioned and audited, and has a database guard is the pattern of
    `gst_forecast` (000272) and `round_scenario`.
  - **Approvals:** the approval engine records an object once a person holding A on its Appendix D row approves it
    (`holder_record`, `share_class`).
  - **Appendix D's "Budgets, forecasts" row:** Owner A, Controller A, Accountant C, Approver C (dept); Auditor,
    Tax agent and Board V; the AI's column says "Proposal (scenario)". It is in `internal/authz` as `Budgets`.
  - **Deferred to this change:** the statements' budget and forecast columns (`p1-reports`), the budget half of the
    close's flux (`p1-general-ledger-close` 6.3), and Home's revenue against budget, cost against budget and forecast
    cash-out tiles (`p1-dashboard-kpis`).
  - **Screens:** Budgets, Forecast, CashForecast and Feasibility are already designed on the insight canvas
    (`docs/design/screens/src/`).

## Goals / Non-Goals

**Goals:**
- The workbook's forecast, scenario levers and feasibility calculator, in exact decimals, agreeing with the reference
  on the workbook's sample and with the oracle on generated inputs.
- Budgets in versions with approval and lock, read by every report and tile that waits for them; C.1 #35.
- A daily cash projection from what the books know today, with expected dates from each contact's history, a buffer
  line, an alert and sandboxed what-ifs.
- Nothing in this change posts, and no AI or MCP tool is added.

**Non-Goals:**
- The rolling forecast, forecast accuracy, the funding plan from the cap table, the board pack and investor updates
  (FR-PLN-04, FR-PLN-07: P2, `p2-reporting-planning`).
- A forecast of Fyniq's own design: payroll by age band and residency, tax by the organisation's Years of Assessment.
  The owner chose the workbook's model; any improvement goes through the PRD first.
- The AI's read tool for scenarios (FR-PLN-08's read lane; `p1-ai-assistant`) and the AI-proposed budget (a catalogue
  task type).
- Department budget holders entering only their department (the Approver's "C (dept)"): members carry no department
  yet. The budget operations are unqualified until they do, as the other "dept" cells are.

## Decisions

### D1. Three packs

- **`sg-cit@2026.1`, selected by Year of Assessment.** Its tables:
  - `rates`: YA from, the rate;
  - `exemptions`: scheme (`sute`, `pte`), tier, amount and percentage. The start-up tax exemption is 75% of the first
    S$100,000 and 50% of the next S$100,000 of chargeable income, for a qualifying company's first three YAs. The
    partial tax exemption is 75% of the first S$10,000 and 50% of the next S$190,000;
  - `rebates`: YA, percentage, cap, and the cash grant's amount with its condition.

  Each row cites IRAS's "Corporate Income Tax Rate, Rebates & Tax Exemption Schemes" and the Budget statement for its
  YA.
- **`sg-cpf@2026.1`, selected by pay month.** Its tables:
  - `rates`: residency group (`sc_spr3`: Singapore citizens and permanent residents from their third year), age from
    and to, the wage from which the rate applies (S$750.01), and the employer and employee rates;
  - `ceilings`: the monthly Ordinary Wage ceiling and the annual salary ceiling, by month from.

  The rows cite the CPF Board's contribution rate tables and its "Ordinary and Additional Wages" page. The graduated
  rates (S$50.01 to S$750) and the first two years of permanent residence are a later version, `sg-cpf@2026.2`, with
  `p1-payroll-link`. Until then the forecast uses only the rows it needs, and payroll uses none.
- **`sg-sdl@2026.1`, selected by pay month.** One table, `levy`: the rate, the minimum and the maximum, by month from.
  The row cites SkillsFuture Singapore's and the CPF Board's SDL pages.
- **Look-ups:** `pack.CITOn(v, ya)`, `pack.CPFOn(v, month, group, age, wage)` and `pack.SDLOn(v, month)`, with
  `ErrNoRule` when no row applies. A forecast month later than every row uses the latest row, and the result says so
  ("based on sg-cpf@2026.1"; ENG-33).
- **Pinned cases** are worked by hand from the official examples:
  - IRAS's start-up and partial exemption worked examples;
  - the CPF Board's examples at S$7,600, at the S$8,000 ceiling and above it, and the ceiling change of 1 Jan 2026;
  - SDL at its minimum, in between, and at its maximum.

  Any value that cannot be confirmed on its official page stops the pack: the loader refuses a file with `[verify]`.

### D2. Exact arithmetic

- **Adding and multiplying** are exact: `apd` with a context wide enough for 60 months of compounding (2,000 digits),
  and a trap on `Inexact`, so a lost digit fails rather than rounds. Customers compound by a churn and a growth rate
  each month, which adds about three digits a month.
- **Division** happens only in named places:
  - depreciation (amount ÷ life);
  - loan repayments (amount ÷ tenor) and interest (× rate ÷ 12);
  - averages (÷ 3, ÷ 12);
  - ratios (gross margin, CAC, LTV, the burn multiple, the profitability index, break-even);
  - payback's interpolation;
  - feasibility's discount factors.

  Each of these is a fraction (a numerator and a denominator, as `internal/domain/kpi`'s `ratio`), divided once when
  it is rounded. No binary floating point and no `math/big`: archtest's money packages include `planning`, `budget`
  and `cashflow`.
- **`RND-PLAN` in `internal/domain/rounding`:**
  - `Plan(x, currency)`: an amount to minor units;
  - `PlanRatio(x)`: 2 decimals;
  - `PlanRate(x)`: 4 decimals of a fraction;
  - `PlanYears(x)`: 1 decimal;
  - `PlanCount(x)`: 1 decimal, for display.

  All are half away from zero. Percentages and margins go through `RND-PERCENT`.
- **Which values are rounded:**
  - **Driver lines** are rounded once from their exact value, worked from the drivers, the exact customers and the
    lines they are a percentage of as shown:
    - subscription, services and other revenue;
    - hosting, AI/API, contractors and payment fees;
    - payroll by function;
    - each other-expense line;
    - depreciation, grants and interest;
    - each year's tax;
    - collections;
    - equity, SAFE and loan receipts, and loan repayments.
  - **Totals add the rounded lines:** total revenue, direct costs, gross profit, payroll, other expenses, EBITDA,
    profit before tax, net profit, payments to suppliers, operating cash flow, net cash flow, cash, net burn, ARR
    (12 × subscription revenue) and the yearly summary.
  - **Shown but not summed:** customers and ARPA, which stay exact.
  - **Rules compare exact values:** the cash-out month, break-even, the buffer alert and the verdict compare the exact
    or the summed figures, never a rounded percentage or ratio.

### D3. The forecast model

The model is the reference's, in this order. Months n = 1 … 60 are active while n ≤ the horizon H, and forecast
year y = ⌈n/12⌉.

- **The start.**
  - The last actual month is the latest closed period's month, else the month before today. A forecast may name its
    own last actual month, as the workbook's setting allows.
  - The forecast starts the month after.
- **From the books, at the last actual month's end, through the balance service:**
  - **cash:** the accounts whose report group is Cash and cash equivalents, plus an optional adjustment;
  - **receivables and payables:** the accounts with the roles accounts-receivable and accounts-payable, as the
    balance sheet shows them;
  - **existing depreciation:** each month's planned depreciation from the fixed-asset register's schedules;
  - **the expense hints:** each report group's average over the last three months (fewer before the books have
    three), the hint beside an other-expense line that names that group by its name, such as Sales & marketing. A
    group whose accounts are all S&M adds its line to S&M for CAC.
- **Customers and revenue:**
  - **customers:**
    - customers at the start = the driver;
    - new customers = new₀ × (1 + growth)ⁿ⁻¹ × the new-customers lever;
    - lost customers = customers at the start of the month × churn × the churn lever;
    - customers at the end = start + new − lost;
  - **ARPA** = ARPA₀ × (1 + price increase)ʸ⁻¹ × the price lever;
  - **subscription revenue** = customers at the end × ARPA;
  - **services** = services₀ × (1 + growth)ⁿ⁻¹ × the services lever;
  - **other revenue** = the driver.
- **Direct costs**, each × the direct-cost lever:
  - hosting = subscription × hosting %;
  - AI/API = total revenue × AI %;
  - contractors = services × contractors %;
  - payment fees = total revenue × fees %;

  plus the payroll of the COGS function.
- **Payroll.** Each headcount row has a role, a function (COGS, R&D, S&M or G&A), a start month, an end month, a
  monthly salary and whether CPF applies.
  - A planned hire after month 1 moves by the hiring-delay lever; the current team does not move.
  - Year y's salary = salary × (1 + the salary increase)ʸ⁻¹.
  - Each row's cost = salary + (with CPF) min(salary, the Ordinary Wage ceiling) × the employer rate for 55 and
    below + SDL, where SDL = min(max, max(min, salary × rate)) on a salary above zero.
  - The figures are from the packs in force in that month (D1).
- **Other expenses.** Each line = (base × (1 + monthly growth)ⁿ⁻¹ + its % × total revenue) × the operating-cost lever.
- **Capex and depreciation.** Each capex row has an amount, a month (month 1 when earlier) and a life in months. Its
  depreciation is amount ÷ life over that many months from its month, added to the existing depreciation.
- **Funding.** Each row has a type (Equity, SAFE, Loan or Grant), a month, an amount, and a yearly rate and a tenor
  for a loan.
  - Equity, SAFEs and loans move by the fundraising-delay lever and scale by the fundraising-amount lever; grants
    arrive as planned.
  - A loan is repaid in equal principal over its tenor from the month after it is drawn, with interest on the
    balance at the start of each month × rate ÷ 12.
- **Profit.** EBITDA = gross profit − payroll − other expenses. Profit before tax = EBITDA − depreciation + grants −
  interest.
- **Tax, by 12-month forecast year:**
  - chargeable = max(0, the year's profit before tax − losses brought forward). The losses are an input for year 1,
    then those carried forward;
  - the exemption is the start-up tiers while y ≤ the start-up years (an input, 3 by default), else the partial tiers;
  - tax = max(0, (chargeable − exemption) × rate − rebate);
  - rate, tiers and rebate come from `sg-cit` for the YA after the calendar year in which the forecast year ends.
    The workbook took one rate and a one-off rebate; for the sample, which has no rebate, the two give the same tax;
  - each year's tax is booked in its last month (or at the horizon) and paid a lag later (an input, 9 months by
    default). A year is paid in month 12k + lag; a partial last year falls beyond the horizon, as in the workbook.
- **Cash:**
  - **collections** = coll₀ × total revenueₙ + coll₁ × total revenueₙ₋₁ + coll₂ × total revenueₙ₋₂, plus 60% of
    the opening receivables in month 1 and 40% in month 2. A ⚠ appears when the three percentages do not total 100%;
  - **payments to suppliers** = −(direct costs other than payroll + other expenses), less the opening payables in
    month 1;
  - **payroll** = −(payroll of every function);
  - operating cash flow adds grants, interest paid and tax paid; net cash flow adds capex, equity and SAFEs, loans
    drawn and loans repaid. Cash closes at opening cash + net cash flow.
- **Unit economics:**
  - net burn = max(0, −(operating cash flow + capex));
  - ARR = 12 × subscription revenue;
  - S&M = S&M payroll + the other-expense line on the Sales and marketing report group;
  - CAC = S&M ÷ new customers;
  - LTV = ARPA × gross margin ÷ (churn × the churn lever);
  - the burn multiple = net burn ÷ the month's increase in ARR, blank without an increase.
- **Key results:**
  - **cash-out month:** the first month cash closes below zero, else "Not within H months";
  - **runway:** the months before it, else "> H";
  - **minimum cash** and its month;
  - **break-even:** the first month with revenue whose EBITDA is not negative, else "Not within forecast";
  - **average burn:** months 1 to 12;
  - **funding gap:** max(0, buffer months × average burn − minimum cash);
  - **revenue** of the next 12 months;
  - **ARR and customers** at the horizon.
- **The yearly summary** sums or ends each forecast year: revenue, gross profit, EBITDA, net profit, net burn, cash,
  customers, ARR and headcount.

### D4. Scenarios and levers

- **The levers:**
  - × new customers, × churn, × price, × services, × other expenses, × direct-cost percentages, × fundraising amount;
  - hiring delay and fundraising delay, in months.
- **Base, Best and Worst** ship with the workbook's values:
  - new customers 1, 1.3, 0.7;
  - churn 1, 0.8, 1.3;
  - price 1, 1.1, 0.9;
  - services 1, 1.2, 0.7;
  - other expenses 1, 0.95, 1.1;
  - direct costs 1, 0.95, 1.1;
  - hiring delay 0, 0, 3;
  - fundraising delay 0, 0, 3;
  - fundraising amount 1, 1, 0.6.

  A forecast may change them.
- **Custom scenarios** have a name, their own levers and, optionally, their own buffer months.
- **Compare** runs every scenario of one forecast and shows their key results and yearly summaries side by side.
- **Funding not yet received (FR-PLN-03).** Every funding row of a forecast is in the future, so not yet received.
  Each scenario is therefore also run without its funding plan. Runway and the cash-out month are shown both ways.
  Home, the tiles and the alerts use the run without, and say "without the funding planned".

### D5. Forecasts are documents

- **The document.** `forecast` is a document type whose versions hold the inputs of D3 and D4 as JSON: money as
  decimal strings in the base currency, rates as decimal strings, months as `YYYY-MM`, the collection pattern as three
  named shares (`this_month`, `next_month`, `month_after`). Left out, the scenarios are Base, Best and Worst. It is
  drafted, changed (ETag and `If-Match`), and recorded. The plan's Go types carry the API's documentation, so the API
  takes and gives them as they are.
- **The guard, FY300.** The database keeps a forecast's keys, a horizon of 12 to 60 months, percentages from 0 to
  100% (churn and growth below 100%), decimal strings with at most 2 decimals for money and 6 for rates, and at most
  100 headcount, 50 capex, 50 funding and 30 other-expense rows.
- **Recording.** A recorded forecast is the company's own: the latest recorded is Home's, as the latest recorded GST
  forecast is the monitor's. A recorded version never changes; the next is a new version. Recording needs C on the
  row, and no approval: a forecast posts nothing (FR-PLN-08).
- **Results** are worked out on every read and never stored, so a closed month or a new pack moves them: `GET
  /v1/forecasts/{id}/results?scenario=`. `POST /v1/forecasts:run` takes the same inputs unsaved, for a sandbox and
  for the AI's later read tool, and saves nothing.

### D6. Budgets

- **The document.** `budget` is a document type for one financial year:
  - a name and a kind (original or revised);
  - a version it revises;
  - lines, each an income or expense account (postable, a revenue, cost-of-revenue, expense or other-income
    account), at most one value per active dimension, and an amount for each month of the year.

  Amounts are decimal strings with cents in the base currency. The guard, FY301, keeps the keys, the months inside
  the year, one line per account and dimension combination, and at most 5,000 lines.
- **Approval and lock.** A budget is drafted and edited by C holders, then approved through the approval engine:
  - **the object:** `budget`, under a person who holds A on the row (the Owner, the Finance controller), as the
    engine's policy and its separation rules say;
  - **approving records it:** a recorded budget never changes, as no recorded document version does;
  - **revising** means copying it to a new draft that revises it;
  - **the year's budget** is the latest recorded version for that year.
- **Copy with growth.** A new draft for the same or the next year. Each amount = round(source × (1 + growth), cents)
  (`RND-PLAN`), with the months moved by a year when the year moves.
- **Import from XLSX.** The first sheet: an account code column, a column per dimension, and twelve month columns
  with `YYYY-MM` headers.
  - **The reader** is the bounded reader of `internal/bankfile` (archive limits and exact decimals, never a binary
    number), moved to `internal/sheet` (`ReadXLSX`), each caller keeping its own words. A month column's header may be
    YYYY-MM, "Jan 2027" or a date; blank cells are nothing; amounts are written at the currency's minor units.
  - **Errors** name the row, the column, what is wrong and how to fix it (ENG-23). Nothing is loaded until every row
    is right.
- **Budget vs Actual (FR-RPT-03).** By account and dimension, for months, a quarter or the year to date: actual from
  the balance service, budget from the year's budget, and the variance with `RND-PERCENT`. Income lines count above
  budget as favourable, cost lines below.
- **The statements' columns (FR-RPT-02).**
  - **Budget:** the P&L by account and its sections, for the statement's months.
  - **Forecast:** the recorded forecast's sections only (revenue, cost of revenue, gross profit, operating expenses,
    profit before tax, net profit), because the forecast's lines are drivers, not accounts.

  A month the source does not cover leaves the column out, as the other comparatives do.
- **The close's flux (FR-GL-14).** The budget half compares each income and expense account's month with the year's
  budget for it, at the same thresholds (`close.variance_amount`, `close.variance_percent`) and in the same exact
  comparison. Its explanations are the flux explanation's new kind `budget`, recording the two figures they explain.
  A month without a recorded budget has no budget variances.
- **Home (FR-KPI-01).** Revenue against budget and cost against budget, for the year to date of the last closed month.
  The forecast cash-out month is the latest recorded forecast's Base scenario, without its funding plan (D4).
- **C.1 #35.** ⚠ for each financial year that has posted entries and no recorded budget. Its drill-down goes to
  Budgets for that year.

### D7. The short-term cash flow

- **The engine** (`internal/domain/cashflow`) projects day by day from today, in the organisation's time zone, for 30,
  60 (default), 90 or 180 days. The weekly view buckets the same days into 13 weeks from today (FR-PLN-05).
- **The inputs in this change:**
  - **opening balance:** the money accounts' ledger balances today, in the base currency, through the balance
    service;
  - **open invoices and bills:** each on its expected date, at its open base amount. A foreign-currency item uses its
    carrying base amount, as the books hold it;
  - **recurring documents:** the next occurrences of each active recurring invoice and bill inside the window, at
    their totals;
  - **payment batches:** approved and not yet released, on their payment date, with their bills taken out of the open
    items so nothing counts twice.
- **Inputs not in this change.** Pay runs and CPF (`p1-payroll-link`), the GST return (at registration) and corporate
  tax (`p2-tax-filings`) join when their documents exist. Until then the page lists them as "not included yet", so the
  lowest point is never taken as complete.
- **Expected dates (rev 0.214):**
  - **with history:** the due date + the contact's average days late over its invoices (or bills) settled in the last
    12 months. Days late = max(0, the day it was settled in full − its due date), so an early payment counts as on
    time, and the average is rounded half away from zero to whole days;
  - **without history:** the due date, or a bill's planned payment date where one is set;
  - **already overdue:** a date before today becomes today.
- **The buffer line** = `planning.buffer_months` × the average monthly net burn (the KPI's `net_burn`) of the last
  three closed months, rounded once. When the company is not burning, there is no line, and the page says so.
- **The lowest point.** The projection gives its lowest closing balance and the first day it occurs, and the first day
  it falls below the buffer.
- **The alert.** The daily job `notify.cash_buffer` (07:40 SGT) sends `cash.below_buffer` to the members who hold C or
  A on the row, once per crossing: it sends again only after the projection has gone back above the buffer. The Inbox
  shows it, and Home's cash tile says it.
- **What-ifs.** `POST /v1/cash-projection:what-if` takes moves (an open item or a recurring occurrence to another day)
  and leaves-outs, and returns the projection without saving anything. A what-if never changes a document, a planned
  payment date or a batch.

### D8. The feasibility calculator

- **The document.** `feasibility_study` holds one idea's inputs, as the workbook's sheet:
  - name, evaluation period (1 to 10 years), discount rate, longest acceptable payback;
  - investment, depreciation life, salvage, working capital as % of revenue;
  - units and their growth, price and its change, variable cost and its change, fixed costs and their growth.

  Its guard is FY302. Studies are drafted, changed and kept, and posting nothing they need no recording step.
- **The tax rate** is `sg-cit`'s headline rate at the study's date, shown, and fixed for the study's years.
- **The model is the reference's:**
  - **each year's cash flows:**
    - after-tax operating profit + depreciation − investment − the change in working capital;
    - plus, at the end of the period, the salvage after tax on its gain over book value, and the release of the
      working capital;
    - a loss saves tax.
  - **NPV and IRR:** NPV discounts at the rate. The IRR is the root of NPV nearest 10% found on the grid from −99% to
    +1,000% in steps of 1%, bisected exactly until its 4-decimal rounding is settled; with no root, "n/a".
  - **payback and discounted payback:** the year the running total turns non-negative, interpolated.
  - **the other results:**
    - the profitability index = 1 + NPV ÷ investment;
    - break-even units = fixed costs ÷ (price − variable cost), "never" when the margin is not above zero;
    - break-even revenue;
    - the margin of safety;
    - the 5 × 5 sensitivity grid (sales volume and fixed costs at 80% to 120%);
    - NPV at 8%, 10%, 12%, 15%, 20%, 25%, 30% and 40%.
  - **the verdict:**
    - ✖ when NPV ≤ 0;
    - ✔ when NPV > 0 and the exact payback is within the longest acceptable;
    - ⚠ otherwise;
    - with no figures entered, the page asks for them.

### D9. Two implementations, and the workbook's sample

- **The oracle.** `oracle/src/oracle/planning.py` is written from this design's rules and `RND-PLAN`, in Python
  `Fraction`s, not from the Go code. The scenario schema gains `forecast` and `feasibility` kinds, with their inputs
  and the books' starting figures given directly.
- **The differential.** Hypothesis generates drivers, plans and levers within the guard's limits. Every figure of every
  scenario must agree to the cent and to the digit (QA-ENG-02). A fixed-seed sample runs in `make test`, the full run
  in `make differential`, and shrunk disagreements become golden cases.
- **The workbook's sample.** A golden fixture, `test/planning/workbook-sample.json`, holds two data sets: the sample
  and the workbook's second data set (`worst_case_data`: Worst, delays, a cash-out, losses, two start-up years, a
  one-off rebate). For each it has the inputs, the starting figures from the workbook's reference ledger and the
  reference model's outputs. `scripts/workbook_planning_fixture.py` generates it from zh-sheet-accounting at a pinned
  commit and records the commit. The parity test asks:
  - each driver line in each month is within a cent of the reference's figure. A line is off by one only where it
    is a percentage of a line already rounded, or adds the register's cents: measured on 8 Nov 2026, every other
    line equals the reference rounded to the cent;
  - each total, cash flow and balance is within a cent per rounded line it adds, cumulatively for cash, since the
    reference sums unrounded values;
  - customers and ARPA agree but for the reference's binary floating point;
  - the cash-out month, runway, minimum month and break-even agree exactly, and the key amounts are within their
    lines' allowance.

  It runs on the CPF, SDL and tax figures the sample used, the 2026 values, with the workbook's one-off rebate as a
  rebate for the first year's YA. (Superseded, 8 Nov 2026: "each driver line equals the reference rounded to the
  cent", which a percentage of a rounded line cannot promise.)

### D10. Access and the API

- **Appendix D's "Budgets, forecasts" row:**
  - **V** (Owner, Controller, Accountant, Approver, Auditor, Tax agent, Board) reads forecasts, budgets, studies,
    results, Budget vs Actual and the cash projection;
  - **C** (Accountant, and the Owner and Controller through A) drafts, changes, runs, records forecasts, imports
    and copies budgets, and keeps studies;
  - **A** (Owner, Controller) approves budgets.

  The operations are unqualified, so the Approver's "C (dept)" does not grant them (Non-Goals).
- **The operations** (each `/v1`, RFC 9457 errors with stable codes, `Idempotency-Key` on writes, ETag and `If-Match`
  on versions):
  - **forecasts:** list, create, get, replace, record, results, compare, `:run`;
  - **budgets:** list, create, get, replace, copy, import, the approval's object;
  - **reports:** Budget vs Actual, and the statements' new `compare=budget|forecast` values (additive enum values);
  - **cash:** the projection and `:what-if`;
  - **feasibility:** studies list, create, get, replace, results.
- **Audit (AUD-01).** Every saved write is audited in its transaction. The runs and what-ifs save nothing, so they
  are not audited, and they are rate-limited as reads (FR-INT-16).
- **Sandboxed (FR-PLN-08).** No operation here writes a journal, an open item, a planned date or a batch. A test runs
  every write and every run, and checks that the ledger and the sub-ledgers did not change.
- **No AI or MCP tool.** The AI's column on the row stays "Proposal (scenario)": the read tool and the proposal come
  with their own changes.

### D11. Settings

- **`planning.buffer_months`:** group Planning, "Cash buffer to keep", a new kind of setting, whole months (`months`),
  from 0 to 36, recommended 6. It is not under dual control: it decides a line and an alert, never money. A forecast scenario may set
  its own.

### D12. Screens

- **The four fragments** are checked against the build, and the insight canvas is republished where the build
  differs (fyniq-openspec Recipe C).
- **The registry:**
  - Budgets, Forecast and Feasibility: `expert`;
  - the short-term cash flow: `basic`, with its what-ifs `expert`;
  - Home's three tiles: in both modes;
  - "Sandbox: nothing here posts" is shown on every planning page in both modes.
- **Each page** has its help keys, its guide page in `docs/user-guide/` (budgets, forecast, cash flow, feasibility)
  compiled into `kb/`, and the states the inventory lists. The cash flow's chart has a table view and a text summary
  (FR-VIZ); amounts are server-rendered strings, never parsed into numbers.

## Risks / Trade-offs

- **The workbook's simplifications are kept** [accountant to confirm]:
  - forecast years are 12-month blocks from the start, not the company's financial years or YAs;
  - every staff member with CPF is at the employer rate for 55 and below, on wages above S$750;
  - tax is paid a fixed lag after each year;
  - opening receivables are collected 60/40.

  The owner chose this model; the guide states each simplification beside the figure it affects.
- **Ties against floating point.** The reference model's floats can land either side of a half-cent tie. The parity
  test uses the oracle's exact value to recognise a tie, rather than widening its tolerance.
- **Cash inputs not yet built.** Without payroll, CPF, GST and tax, the projection's lowest point can look better than
  it is. The page lists the missing inputs, and the alert says it is based on the inputs Fyniq has.
- **Exact digits.** Sixty months of compounding grow exact decimals to a few hundred digits. Each result reads at most
  60 months × a few dozen lines, measured in the engine's benchmark. `Inexact` is a failure, never a rounding.
- **Statutory values.** `sg-cit`'s YA2026 rebate and the CPF ceilings must be confirmed on IRAS's and the CPF Board's
  pages before the packs ship. Until then the facts table in the `fyniq-sg-statutory` skill is a snapshot, not a
  source.

## Migration Plan

- Each slice is one pull request on its own `make test`, in the order of `tasks.md`.
- Migrations add the document types and guards (FY300 to FY302), each with its attack test. They widen
  `document_type_known` NOT VALID and validate it in the next migration, as 000272 and 000273 did. They also add the
  approval object `budget` and the flux explanation's `budget` kind.
- Rollback: the slices add only new types, operations and settings; reverting a slice removes them with nothing
  posted to undo.

## Open Questions

None for the owner. The decisions are rev 0.214's; the accountant's confirmations are tagged where they apply.
