# Proposal

## Why

The workbook planned the company ahead on three sheets: a driver-based forecast with Base, Best and Worst scenarios,
a feasibility calculator for one investment idea, and the budget that the dashboard and the Budget vs Actual report
compared with the books. Fyniq does not have any of them yet. Several modules already built say "arrives with
`p1-planning`":
- the statements' budget and forecast columns (FR-RPT-02);
- the Budget vs Actual report (FR-RPT-03);
- the budget half of the close's flux analysis (FR-GL-14);
- Home's revenue against budget, cost against budget and forecast cash-out tiles (FR-KPI-01).

Nothing yet shows the founders where cash will be in 60 days, or warns them before it falls below what they want to keep.

This change builds PRD-001 §8.14 for launch:
- **Budgets:** FR-PLN-01 and C.1 #35.
- **The forecast:** the driver-based forecast and its outputs (FR-PLN-02, FR-PLN-03).
- **Cash:** the short-term cash flow and its 13-week view (FR-PLN-09, FR-PLN-05).
- **Feasibility:** the feasibility calculator (FR-PLN-06).
- **Sandboxed:** none of it ever posts (FR-PLN-08).

The rolling forecast and forecast accuracy (FR-PLN-04) and the funding plan from the cap table (FR-PLN-07) are P2
(`p2-reporting-planning`).

## What Changes

- **The owner's decisions of 8 Nov 2026, taken interactively (PRD-001 rev 0.214):**
  - **The forecast's maths (FR-PLN-02, RND-PLAN).** The workbook's own model, as its reference model states it
    (zh-sheet-accounting `tests/ref_planning.py`), in exact decimals under the new rounding policy `RND-PLAN`.
    - Customers stay fractional, as in the workbook.
    - Employer CPF, SDL and corporate tax come from statutory packs.
    - It is checked against the reference on the workbook's sample to the cent.

    The alternatives were whole customers, which drifts from the workbook in every month that has churn, and a model
    of Fyniq's own, which would leave the founders' sheet without a check.
  - **The cash buffer (FR-PLN-03, FR-PLN-09).** A number of months of average monthly net burn: the setting
    `planning.buffer_months`, 6 by default, as the workbook and C.1 #36. A scenario may set its own number of months.
    The funding gap is buffer × average burn − minimum cash, at least 0.

    The alternatives were a fixed amount, which goes stale as the company grows, and both, which makes two numbers to
    keep in step.
  - **Expected payment dates (FR-PLN-09).** An open invoice's or bill's expected date is its due date plus the
    contact's average days late over the documents settled in the last 12 months, an early payment counting as on
    time. With no such history it is the due date, or the planned payment date where one is set.

    The alternatives were the due date alone, which is too hopeful for late payers, and a median or a weighted
    average, which is harder to explain on the screen.
- **Statutory packs (ENG-30, §10.6):** three new packs, each value with its official source and pinned cases worked
  from the official examples (QA-ENG-08):
  - **`sg-cit@2026.1`**, by Year of Assessment (IRAS): the corporate tax rate, the start-up tax exemption and the
    partial tax exemption tiers, and each YA's rebate and cap with its conditions;
  - **`sg-cpf@2026.1`**, by pay month (CPF Board): the employer and employee rates by age band for Singapore citizens
    and permanent residents from their third year, on wages above S$750; the Ordinary Wage ceiling; and the annual
    salary ceiling. The graduated rates and the first two years of permanent residence come with `p1-payroll-link`, as
    `sg-cpf@2026.2`;
  - **`sg-sdl@2026.1`**, by pay month (SkillsFuture Singapore and CPF Board): the rate, the minimum and the maximum.
- **The forecast (FR-PLN-02, FR-PLN-03, FR-PLN-08):**
  - **Scenarios.** A forecast is a document that holds:
    - the drivers;
    - the headcount, capex, funding and other-expense plans;
    - Base, Best and Worst, with the workbook's lever values, plus custom scenarios;
    - a horizon of 12 to 60 months.
  - **Where the starting figures come from.**
    - **From the books:** the starting cash, receivables and payables at the last closed month, the existing assets'
      depreciation, and each expense line's average over the last three months.
  - **Outputs.** Every scenario gives:
    - the monthly P&L and cash and the yearly summary;
    - the tax estimate under the start-up or the partial tax exemption;
    - the key results: cash-out month, runway, minimum cash and its month, break-even month, average burn, funding
      gap, next 12 months' revenue, and ending ARR and customers.

    Scenarios can be compared side by side. With funding not yet received, runway and the cash-out month are shown
    with and without it; Home and alerts use the figure without it, and say so.
  - **Recording.** A recorded forecast is the company's own, and Home shows the latest. Drafts are sandboxes. Nothing
    posts.
- **Budgets (FR-PLN-01, C.1 #35):**
  - **What a budget holds.** One financial year's monthly amounts by income and expense account and dimension value,
    in versions (original, revised).
  - **Approval and lock.** An Owner or a Finance controller approves a version, and the approved version is locked.
    The year's budget is its latest approved version.
  - **Copy and import.** A version can be copied with growth, or imported from XLSX.
  - **C.1 #35** warns of a financial year with postings and no approved budget.
- **What reads the budget and the forecast:**
  - the statements' budget and forecast columns (FR-RPT-02);
  - the Budget vs Actual report by account and dimension (FR-RPT-03);
  - the budget variances of the close's flux analysis, at the close's own thresholds (FR-GL-14);
  - Home's revenue against budget, cost against budget and forecast cash-out tiles (FR-KPI-01).
- **The short-term cash flow (FR-PLN-09, FR-PLN-05):**
  - **The projection.** Daily for 30, 60, 90 or 180 days (60 by default), in weekly buckets as the 13-week view. It
    starts from the money accounts' balances today and adds:
    - open invoices and bills on their expected dates;
    - the next occurrences of recurring invoices and bills;
    - approved payment batches not yet released.

    Pay runs, CPF, GST and tax due dates join with their modules; until then the screen says which inputs are not
    included yet.
  - **The lowest point and the buffer.** The projection shows its lowest balance and that day. The buffer line is
    `planning.buffer_months` × the average net burn of the last three closed months. A daily job alerts the planners
    when the projection falls below it.
  - **What-ifs.** "Delay this bill" or "chase that invoice" is worked out without saving anything.
- **The feasibility calculator (FR-PLN-06):**
  - **What it holds.** A study is a document of one idea's inputs, as on the workbook's sheet.
  - **What it gives:** NPV, IRR, payback, discounted payback, the profitability index, break-even units and revenue,
    the margin of safety, the sensitivity grid, NPV at other discount rates and the verdict.
  - **Tax.** The rate is the headline corporate tax rate from `sg-cit`.
- **Two implementations agree (golden rule 3):** an independent model in the Python oracle is written from the
  rules, not from the Go code. The Go engine and the oracle are compared on generated forecasts and feasibility studies
  (QA-ENG-02). A golden fixture of the workbook's sample, with the workbook reference model's outputs, pins the parity.
- **Screens:** Budgets, Forecast, Short-term cash flow and Feasibility, already designed on the insight canvas, plus the
  Home tiles. Each comes with its help keys, guide pages compiled into the knowledge base and a threat model.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs: `planning/budgets-and-forecasts` (FR-PLN-01, FR-PLN-02,
FR-PLN-03, FR-PLN-05, FR-PLN-06, FR-PLN-08, FR-PLN-09), `ledger/money-and-rounding` (RND-PLAN) and `controls/checks`
(C.1 #35). This change sets `skip_specs: true`.

### Modified Capabilities

None in this change. Rev 0.214's decisions are already in the main specs (FR-PLN-02, FR-PLN-03, FR-PLN-09 and RND-PLAN,
stamped). If building shows a requirement is wrong, the PRD changes first (fyniq-openspec Recipe B).

## Impact

- **Packs:** `packs/sg-cit/2026.1`, `packs/sg-cpf/2026.1`, `packs/sg-sdl/2026.1`; `internal/domain/pack` gains their
  schemas, checks, look-ups and pinned-case runners.
- **Domain:**
  - `internal/domain/planning`: the forecast model, scenarios and the feasibility model;
  - `internal/domain/budget`: versions, copy with growth, XLSX import and the year's budget;
  - `internal/domain/cashflow`: the short-term projection, expected dates and what-ifs;
  - `internal/domain/rounding`: `RND-PLAN`.

  All of them are in archtest's money packages, and they read balances only through the balance service (ENG-04).
- **Database:**
  - the document types `forecast`, `budget` and `feasibility_study`, with their guards (FY300 to FY302);
  - the budget approval object;
  - the flux explanation's budget kind.
- **API:** the forecast, budget, cash projection and feasibility operations, `POST /v1/forecasts:run` and
  `POST /v1/cash-projection:what-if` (they save nothing), the budget and forecast comparatives on statements, the
  Budget vs Actual report, and the authorisation-matrix rows on Appendix D's "Budgets, forecasts" row.
- **Checks and jobs:** C.1 #35; the `notify.cash_buffer` job and the `cash.below_buffer` notification.
- **Settings:** `planning.buffer_months`.
- **Oracle:** `oracle/src/oracle/planning.py` and its scenario kinds; a golden fixture of the workbook's sample.
- **Web:** the four planning screens, Home's three tiles, and the statements' and the close's budget columns.
- **Docs:** user-guide pages for budgets, the forecast, the cash flow and feasibility, compiled into `kb/`; a threat
  model; the build plan's wave-5 paragraph.
- **No AI or MCP surface in this change.** FR-PLN-08's read lane for scenarios is a read tool that `p1-ai-assistant`
  adds (wave 6), and the AI-proposed budget is a catalogue task type that arrives later. Neither is added here.
