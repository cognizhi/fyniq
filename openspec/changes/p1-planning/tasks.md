# Tasks

Each slice below is one pull request that passes `make test` on its own. Each one carries:
- its migration and attack tests;
- its API, with authorisation-matrix rows;
- its screens, with help keys and guide sections compiled into the knowledge base;
- the hand-worked, pinned or differential tests behind every figure it shows.

Before each slice, load `fyniq-feature-change` and `fyniq-design-system`. Load `fyniq-sg-statutory` for slice 1 and
`fyniq-accounting-domain` and `fyniq-quality-gates` for slices 2 and 7.

## 1. The decisions and the packs (slice 1)

- [x] 1.1 PRD-001 rev 0.214 records the owner's decisions of 8 Nov 2026, and the main specs are updated and stamped
  (`python3 scripts/prdspec.py stale` is clean, `make check` passes):
  - FR-PLN-02: the workbook's model, exact, customers fractional, `RND-PLAN`;
  - FR-PLN-03: the buffer is `planning.buffer_months` months of burn, 6;
  - FR-PLN-09: expected dates by average lateness over 12 months.

  `// req: FR-PLN-02`, `// req: FR-PLN-03`, `// req: FR-PLN-09`, `// req: RND-PLAN` (spec text)
- [x] 1.2 `sg-cit@2026.1` (D1): `rates`, `exemptions` and `rebates` from IRAS's pages and the Budget statements, each
  row with its source, quote and retrieval date, and `pack.CITOn`. The pinned cases are IRAS's start-up and partial
  exemption examples, and each YA's rebate at and above its cap. `go test ./internal/domain/pack` loads it and passes
  them. `// req: ENG-30`, `// req: QA-ENG-08`
- [x] 1.3 `sg-cpf@2026.1` (D1): `rates` for `sc_spr3` by age band on wages above S$750, and `ceilings` (the Ordinary
  Wage ceiling, the annual salary ceiling) from the CPF Board's pages, with `pack.CPFOn`. The pinned cases are worked by
  hand from the CPF Board's tables: S$7,600, S$8,000 and S$12,000 at 55 and below, one per other age band, and the
  ceiling's change on 1 Jan 2026. The skill's table of what the pack holds is updated. `// req: ENG-30`,
  `// req: QA-ENG-08`
- [x] 1.4 `sg-sdl@2026.1` (D1): `levy` (rate, minimum, maximum) and `pack.SDLOn`, pinned at S$500 (the minimum),
  S$2,000 and S$6,000 (the maximum). `// req: ENG-30`, `// req: QA-ENG-08`

## 2. The forecast engine and its oracle (slice 2)

- [x] 2.1 `RND-PLAN` in `internal/domain/rounding` (D2): `Plan`, `PlanRatio`, `PlanRate`, `PlanYears`, `PlanCount`,
  with table tests at each tie, both signs and zero. `// req: RND-PLAN`
- [x] 2.2 `internal/domain/planning` (D2, D3, D4): the inputs, the exact context with its `Inexact` trap, the monthly
  model, tax by forecast year, cash, unit economics, key results and the yearly summary.
  - Scenarios, levers and the run without funding.
  - `planning`, `budget` and `cashflow` are added to archtest's money packages.
  - The tests are hand-worked: a month of each line, a tax year under each exemption, a loan, a delayed hire, a
    delayed raise, the 60/40 opening receivables, and the ⚠ on collections not totalling 100%.

  `// req: FR-PLN-02`, `// req: FR-PLN-03`, `// req: RND-PLAN`
- [x] 2.3 The oracle (D9): `oracle/src/oracle/planning.py`, written from the design's rules in `Fraction`s, with the
  scenario schema's `forecast` kind, `test/differential/` updated, and hand-worked tests of its own. A Hypothesis
  strategy generates forecasts within FY300's limits, and the fixed-seed sample runs in `make test`. `make
  differential` runs 1,000 forecasts with no disagreement. `// req: QA-ENG-02`, `// req: FR-PLN-02`
- [x] 2.4 The workbook's sample (D9):
  - a script, `scripts/workbook_planning_fixture.py`, generates `test/planning/workbook-sample.json` from
    zh-sheet-accounting at a pinned commit;
  - the parity test asks for driver lines within a cent, totals within a cent per rounded line they add, and the key
    results' months exactly (design D9, as measured on 8 Nov 2026).

  `go test ./internal/domain/planning -run TestTheWorkbooksSample` passes. `// req: FR-PLN-02`

## 3. Forecasts as documents (slice 3)

- [x] 3.1 The `forecast` document type and its guard, FY300 (D5), with the type widened NOT VALID and validated in the
  next migration. The attack tests cover keys, limits, decimal strings and recorded versions that never change.
  `TestAttackForecastsKeepTheirRules` `// req: FR-PLN-02`, `// req: FR-PLN-08`
- [x] 3.2 The API (D5, D10): forecasts list, create, get, replace and record; results by scenario; compare; and
  `POST /v1/forecasts:run`, which saves nothing. With them come the matrix rows, audit on every write, `make openapi`
  and the web build. `TestForecastsThroughTheAPI` runs Base, Best, Worst and a custom scenario, and the run without
  funding, from the books' starting figures on the sample company. `// req: FR-PLN-02`, `// req: FR-PLN-03`,
  `// req: FR-PLN-08`
- [x] 3.3 `planning.buffer_months` (D11), with its guide section, its KB page and a scenario's own buffer. The setting
  test covers its range and recommended value. `// req: FR-PLN-03`
- [x] 3.4 Sandboxed: every forecast write and run leaves the ledger and the sub-ledgers as they were: the ledger's
  revision is unchanged after them, in `TestForecastsThroughTheAPI`. `// req: FR-PLN-08`

## 4. Budgets (slice 4)

- [x] 4.1 The `budget` document type and its guard, FY301 (D6), and the approval object `budget`, with its policy and
  separation rules. The attack tests cover keys, months inside the year, account kinds, duplicate lines, the
  5,000-line limit and recorded budgets that never change. `TestAttackBudgetsKeepTheirRules` `// req: FR-PLN-01`
- [x] 4.2 The API (D6, D10): budgets list, create, get, replace, copy with growth (`RND-PLAN` per amount) and the
  approval; the year's budget is the latest recorded. With them come the matrix rows and audit.
  `TestBudgetsThroughTheAPI` covers original and revised versions, approval by A, refusal to the preparer where the
  policy separates, and the lock. `// req: FR-PLN-01`
- [x] 4.3 Import from XLSX (D6): the bounded reader moved to a shared package, with the bank import still passing its
  tests. Every row error names its row, column and fix, and nothing loads until every row is right. A zip bomb, a
  binary number and a month outside the year are refused. in `TestBudgetsThroughTheAPI` and `TestTheSheetsHeader` `// req: FR-PLN-01`, `// req: ENG-23`
- [x] 4.4 C.1 #35 (D6): ⚠ per financial year with postings and no recorded budget, with its drill-down, and passing
  when every such year has one, in `TestBudgetsThroughTheAPI` `// req: C.1 #35`

## 5. What reads the budget and the forecast (slice 5)

- [x] 5.1 Budget vs Actual (D6): by account and dimension, for months, a quarter and the year to date, with
  favourable and unfavourable by line kind. Every figure is hand-worked on the sample company.
  `TestBudgetVsActual` `// req: FR-RPT-03`, `// req: QA-RPT-01`
  - As built: `GET /v1/reports/budget-vs-actual?from=&to=&by=` (any run of months in one financial year, so a
    quarter is its three months), worked by hand in `TestBudgetVsActualAndHome` on its own small company rather
    than the sample's, which has no budget yet.
- [x] 5.2 The statements' `compare=budget` (by account) and `compare=forecast` (by section) columns (D6), left out
  where the source does not cover a month. The enum values are additive (oasdiff clean). `TestStatementComparatives`
  `// req: FR-RPT-02`
  - As built (rev 0.220): the budget is a column labelled Budget, its figures opening no ledger lines. The forecast is
    the statement's `forecast` object, a figure per section beside the columns, since a column needs one per account.
    The export takes the budget column and not yet the forecast.
- [x] 5.3 The close's budget variances (D6): the `budget` kind of flux explanation (migration and guard), at the
  close's thresholds, stale when either figure changes. `TestTheBudgetFlux` `// req: FR-GL-14`
  - As built (rev 0.221): migrations 000281 and 000282 add the `kind` column and its check, and the guard is
    unchanged. `listPeriodVariances` and `explainVariance` take `against=budget`. The close page shows "Variances
    against the budget" as a second card. The C.2 #57 check counts both kinds.
- [x] 5.4 Home's revenue against budget, cost against budget and forecast cash-out tiles (D4, D6), with "without the
  funding planned" shown and each tile's undefined state ("No budget for 2027"). `TestHomePlanningTiles`, and the web
  tests. `// req: FR-KPI-01`, `// req: FR-PLN-03`
  - As built: `GET /v1/planning/summary?month=`, tested in `TestBudgetVsActualAndHome` and the web's "Home's tiles
    from the plans". Home asks for the KPIs' month, so the tiles agree with the others; the budget tiles are Expert's,
    the cash-out month shows in both modes.

## 6. The short-term cash flow (slice 6)

- [x] 6.1 `internal/domain/cashflow` (D7): the inputs, expected dates, daily and weekly buckets, the lowest point, the
  buffer line and the inputs not yet included. The hand-worked tests cover:
  - a late payer, an early payer, no history, a planned payment date and an overdue item;
  - a batch whose bills are not counted twice;
  - a foreign-currency item;
  - not burning.

  `// req: FR-PLN-09`, `// req: FR-PLN-05`
  - As built (rev 0.222):
    - The pure engine is tested in `TestTheProjection` and `TestAverageLate`; settlements in `TestSettledInFull`.
    - The cases that need the books are in `TestTheCashProjectionThroughTheAPI` and `TestTheCashProjectionNotBurning`.
    - The opening is the daily cash position (FR-BNK-13).
    - A recurring document's occurrences are placed at the total its content comes to today.
- [x] 6.2 The API (D7, D10): the projection (30, 60, 90 or 180 days, or 13 weeks) and `POST
  /v1/cash-projection:what-if`, which saves nothing. `TestTheCashProjectionThroughTheAPI` checks that a what-if
  changes no document. `// req: FR-PLN-09`, `// req: FR-PLN-08`
  - As built: `GET /v1/cash-projection?days=` (`91` is the 13 weeks) and the what-if take moves by movement key. The
    test checks that the ledger revision and the bill's planned date did not change.
- [x] 6.3 The `notify.cash_buffer` job and the `cash.below_buffer` notification (D7): sent once per crossing to C and A
  holders, again after a recovery, and in the Inbox. `TestTheCashBufferAlert` `// req: FR-PLN-09`, `// req: FR-NTF-01`
  - As built (rev 0.223): a recovery is told too (`cash.buffer_recovered`), which is how the next crossing is known. The
    notices link to Home until the cash-flow screen, and Home's cash tile says it with the screens (task 8.1).

## 7. The feasibility calculator (slice 7)

- [x] 7.1 The feasibility model in `internal/domain/planning` (D8): cash flows, NPV, the exact IRR on the grid with
  bisection, payback and discounted payback, the profitability index, break-even, the margin of safety, the
  sensitivity grid, NPV at the eight rates and the verdict. Hand-worked tests cover each verdict, no IRR, two IRRs, no
  margin and no payback. `// req: FR-PLN-06`, `// req: RND-PLAN`
  - As built (rev 0.224): `TestTheFeasibilityCalculator` and `TestTheIRR`. Each present value is rounded once and the NPV
    adds them. The IRR's root is found on the exact polynomial, and all-zero flows have none.
- [x] 7.2 The oracle's `feasibility` kind and its Hypothesis strategy (D9). The fixed-seed sample runs in `make test`,
  and `make differential` stays clean. The workbook sample's feasibility results agree. `// req: QA-ENG-02`,
  `// req: FR-PLN-06`
  - As built:
    - The format is `fyniq-feasibility@1` (`oracle/src/oracle/feasibility.py`), with seven golden studies, two of them
      the workbook's.
    - The gate is `TestTheFeasibilityCalculatorAgreesWithTheOracle`, with `feasibility` in `DIFFERENTIAL_SOURCES`.
    - Workbook parity is checked in `TestTheWorkbooksFeasibility`.
- [x] 7.3 The `feasibility_study` document type, its guard (FY302) and the API: list, create, get, replace and results,
  with the tax rate from `sg-cit`. `TestFeasibilityThroughTheAPI` and `TestAttackFeasibilityStudiesKeepTheirRules`.
  `// req: FR-PLN-06`, `// req: FR-PLN-08`
  - As built (rev 0.225): the study keeps its date, its first version's, so the tax rate (for the YA after it) stays
    fixed. Problems are STUDY-01 by field. The results give the verdict in plain words.

## 8. The screens (slice 8)

- [x] 8.1 Budgets, Forecast, Short-term cash flow and Feasibility (D12), built on the generated client:
  - registry tags, help keys and the sandbox label in both modes;
  - the inventory's states;
  - the chart's table view and summary;
  - no figure parsed into a number.

  The component tests use axe, and mode parity is checked. `// req: FR-PLN-01`, `// req: FR-PLN-02`,
  `// req: FR-PLN-03`, `// req: FR-PLN-05`, `// req: FR-PLN-06`, `// req: FR-PLN-09`, `// req: QA-UI-04`,
  `// req: QA-UI-05`
  - Built so far:
    - (rev 0.226) Cash ahead (`/planning/cash`, Basic, with Expert what-ifs) and Home's Cash ahead tile;
    - (rev 0.227) Feasibility (`/planning/feasibility`, Expert), with the inputs as the workbook's sheet;
    - (rev 0.228) Budgets (`/planning/budgets`, Expert). The paste-and-fill grid and AI-proposed versions wait;
    - (rev 0.229) Forecast (`/planning/forecast`, Expert): plan editor, record step, scenario results and comparison.
- [x] 8.2 The statements' budget and forecast columns, Budget vs Actual on Reports, and the close's budget variances in
  the web app. `// req: FR-RPT-02`, `// req: FR-RPT-03`, `// req: FR-GL-14`
  - As built:
    - the statements' columns with task 5.2 (rev 0.220);
    - the close's budget variances with task 5.3 (rev 0.221);
    - Budget vs Actual at `/reports/budget-vs-actual` (rev 0.228).
- [x] 8.3 The guide pages `docs/user-guide/budgets.md`, `forecast.md`, `cash-flow.md` and `feasibility.md`, with the
  workbook simplifications stated beside their figures. They are compiled into `kb/pages/`, followed by `make
  kb-index`, `make kb-lint` and a log entry.
  - As built: `cash-flow.md` (`planning.cash`), `feasibility.md` (`planning.feasibility`), `budgets.md`
    (`planning.budgets`) and `forecast.md` (`planning.forecast`), each compiled into `kb/pages/task/`.
- [ ] 8.4 The four fragments checked against the build and the insight canvas republished where they differ (Recipe C).
  `python3 docs/design/screens/build.py check` passes, and the new canvas version is recorded.
- [ ] 8.5 The threat model `docs/security/threat-models/planning.md`: forecasts, budgets and the cash projection, which
  reads every open item. It covers what-ifs that save nothing, the alert's audience, budget approval and the absence
  of any AI or MCP tool.

## 9. As built (slice 9)

- [ ] 9.1 `make test` and `make up` pass on the last slice, and `make differential` is clean. The build plan's
  p1-planning paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-planning`, then `/opsx:archive p1-planning --skip-specs` once every task above is ticked.
- `p1-statutory-packs` task 4.1 notes that `sg-cpf@2026.1` and `sg-sdl@2026.1` exist, and that the graduated rates
  wait for `p1-payroll-link`.
