# Tasks

## 1. The KPI definitions (FR-KPI-02, FR-KPI-03, RND-KPI)

- [ ] 1.1 `RND-KPI` in `internal/domain/rounding`, with property tests: once, half away from zero, by kind.
  `// req: RND-KPI`
- [ ] 1.2 `internal/domain/kpi`: every definition of design D1, versioned, with its source, formula, date basis,
  unit, decimals and undefined states; each worked by hand on a small month, and the workbook's carried-over states
  (G4-047, G4-048). `// req: FR-KPI-02`, `// req: FR-KPI-03`
- [ ] 1.3 (oracle session) The KPIs of the sample company in the independent oracle, from §11.5's words.
  `// req: QA-ENG-02`

## 2. Entered counts (rev 0.161)

- [ ] 2.1 Migration `kpi_input` (RLS forced, audited, whole numbers ≥ 0, replaced not deleted), the storage port and
  adapter, and the attack tests. `// req: FR-KPI-02`
- [ ] 2.2 The API: `listKpiInputs` and `setKpiInput` (Budgets C), with the matrix rows.

## 3. The API

- [ ] 3.1 `getKpis` for a month at one revision: each KPI's value or reason, its definition and version, and the month
  before; the reports' view and the board pack's (D7); the matrix rows. `// req: FR-KPI-02`, `// req: FR-KPI-03`

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
