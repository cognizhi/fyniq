# Tasks

## 1. The KPI definitions (FR-KPI-02, FR-KPI-03, RND-KPI)

- [x] 1.1 `RND-KPI` in `internal/domain/rounding`, with property tests: once, half away from zero, by kind.
  `// req: RND-KPI`
  (29 Oct 2026: `rounding.KPI`; `TestKPIRoundsOnceHalfAwayFromZero`, cases and a rapid property.)
- [ ] 1.2 `internal/domain/kpi`: every definition of design D1, versioned, with its source, formula, date basis,
  unit, decimals and undefined states; each worked by hand on a small month, and the workbook's carried-over states
  (G4-047, G4-048). `// req: FR-KPI-02`, `// req: FR-KPI-03`
  (29 Oct 2026: `internal/domain/kpi`: 27 definitions in the KPI page's order, each `key@1`; `Compute` from exact
  month figures, `Read` from the P&L, balance sheet and cash flow by month at one revision (receivables, payables and
  cash by each account's cash-flow line; booked MRR from revenue accounts whose report group names a subscription
  stream, [accountant to confirm]). The no-counts reason begins "no subscription data" (G4-047). Tests:
  `TestTheKPIsWorkedByHand`, `TestWithoutCountsTheCustomerKPIsHaveNone`, `TestTheWorkbooksUndefinedStatesCarryOver`,
  `TestEveryDefinitionStatesItself`.)
- [ ] 1.3 (oracle session) The KPIs of the sample company in the independent oracle, from §11.5's words.
  `// req: QA-ENG-02`

## 2. Entered counts (rev 0.161)

- [x] 2.1 Migration `kpi_input` (RLS forced, audited, whole numbers ≥ 0, replaced not deleted), the storage port and
  adapter, and the attack tests. `// req: FR-KPI-02`
  (29 Oct 2026: migration 000192; FY244; `storage.KPIInputs` (Get, Range, Set by version); a month entered meanwhile is
  CodeVersion. Test: `TestAMonthsKPICountsAreEnteredAndChangedAsVersions`, with the grants and FY244.)
- [x] 2.2 The API: `listKpiInputs` and `setKpiInput` (Budgets C), with the matrix rows.
  (29 Oct 2026: `PUT /v1/kpi-inputs/{month}` with If-Match ("0" for a month without counts), the three customer counts
  together; `GET /v1/kpi-inputs`, paged.)

## 3. The API

- [x] 3.1 `getKpis` for a month at one revision: each KPI's value or reason, its definition and version, and the month
  before; the reports' view and the board pack's (D7); the matrix rows. `// req: FR-KPI-02`, `// req: FR-KPI-03`
  (29 Oct 2026: `GET /v1/kpis?month=&as_at_revision=`, `kpis` with each definition, the month and the month before, and
  the month's counts. Test: `TestKPIsThroughTheAPI`, every November figure worked by hand, October's before.)

## 4. The screens

- [ ] 4.1 Home as the dashboard (D5): the pulse card, the tiles with their definitions and drill-down, Basic's four,
  the three charts; role variants as built. `// req: FR-KPI-01`
- [ ] 4.2 `/reports/kpis` (D6): the month's KPIs and the month before, definitions, both MRR measures, and the
  entered counts' editor. Vitest with axe; mode parity.
- [ ] 4.3 Help keys, the guide pages `home.overview` and `reports.kpis`, compiled into `kb/`.
- [ ] 4.4 Design check of `Home.html` and `Kpis.html` against the build; what joins later is listed.

## 5. Finish

- [ ] 5.1 The review register, if the subscription-revenue reading stays [accountant to confirm] (its own PR).
- [ ] 5.2 `make check`, `make test` and `make test-e2e`; archive.
