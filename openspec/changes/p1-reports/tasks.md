# Tasks

## 1. The statements (FR-RPT-01, FR-RPT-02, FR-RPT-08, INV-10, INV-11)

- [x] 1.1 The balance service reads as at a revision (design D4): the lines up to a ledger revision, summed by
  account and period, through a storage query. One snapshot gives a statement its revision.
  - Tests: a back-dated posting after the revision is left out (G4-051); a re-run at the revision gives the same
    figures. `// req: FR-RPT-08`, `// req: ENG-46`
  (28 Oct 2026: migration 000190's `fyniq.balances_at(period, revision)`; `Balances.ForPeriodAt`; `balance.At`,
  `TrialAt` and `PositionAt`. Tests: `TestTheStatementsLaidOutFromTheChart`, `TestFinancialStatementsThroughTheAPI`.)
- [ ] 1.2 `internal/domain/statement`: the P&L, balance sheet, indirect cash flow and trial balance laid out from the
  chart (D2) for a run of months, with the prior period, the prior year, and columns by month (D1).
  - Its checks (D3): the balance sheet balances; every account in one statement; the cash flow ties to the money
    accounts; equity reconciles.
  - Each figure is worked by hand on the sample company, and the oracle's statement totals are written by the oracle
    session.
  - Tests: `// req: FR-RPT-01`, `// req: FR-RPT-02`, `// req: INV-10`, `// req: INV-11`, and the non-cash case (a cost a
    director paid personally).
  (28 Oct 2026, part: `internal/domain/financials` lays out the P&L, the balance sheet and the trial balance from the
  chart, with the prior period, the prior year, or a column per month; the checks `balances` and `placed` (INV-10).
  Test: `TestTheStatementsLaidOutFromTheChart`, worked by hand. **Left:** the cash flow and the movement in equity
  (INV-11), and the oracle's statement totals.)
- [ ] 1.3 The API: the statement for its kind, period, comparison, columns and revision, with its checks, framework
  title (FR-ORG-10), revision, parameters and platform version, and "restated presentation" when the chart changed after
  the revision. Permissions per D9. `make openapi` first.
  - Tests: through the API, the matrix rows, and a board-pack reader refused the trial balance. `// req: FR-RPT-08`
  (28 Oct 2026, part: `GET /v1/reports/statements/{kind}` for `pl`, `bs` and `tb`, with `from`, `to`, `compare`,
  `columns` and `as_at_revision`; REPORT-03 and REPORT-04; the year's title, management statements without the
  financial years set; `restated_presentation` from the accounts' `changed_at` (migration 000190). A qualified reader
  is refused until the board pack (D9). **Left:** the cash flow.)
- [ ] 1.4 The close's `statements` check (D3). `// req: INV-10`, `// req: INV-11`
  (28 Oct 2026, part: ⚠ when an account with a balance sits in no section of the month's balance sheet; the close's
  guide and kb page say so. **Left:** the cash flow's tie to the money accounts and equity's movement, with 1.2.)

## 2. The screens

- [ ] 2.1 Design check: `Reports.html`, `FinancialStatement.html` and `ExportDialog.html` against what the API
  gives. Update a fragment where the build differs, and republish the canvas (fyniq-openspec Recipe C).
- [ ] 2.2 `reports-index`: Basic's curated set with a line each, and Expert's full list, the accountant exports among
  them.
- [ ] 2.3 `financial-statement`: the toolbar (period, comparison, columns, as at), the statement with its groups and
  totals, and every figure opening its general ledger lines (D8, FR-UX-04). Basic shows plain words under each section;
  Expert adds codes, variances and functions. The checks are shown, the framework title is shown, and the FR-UX-41
  banner shows through the review register.
  - Tests: Vitest with axe on every state; mode parity (QA-UI-04).
- [ ] 2.4 Help keys, and the guide pages `reports.statements` and `reports.index`, compiled into `kb/`.

## 3. Exports (FR-RPT-05, FR-RPT-09, FR-RPT-10)

- [ ] 3.1 The CSV and XLSX writers (D5): typed numbers, a data tab, formula-safe text.
  - Tests: an XLSX opened and parsed back, and a name that starts with =, + or @. `// req: FR-RPT-05`
- [ ] 3.2 The PDF through the render service: a Typst statement template, titled with the framework, with the revision
  in the footer. `// req: FR-RPT-05`
- [ ] 3.3 Background exports on the outbox:
  - an export record and its file in the blob store;
  - a link valid 24 hours (a setting), re-checked at each download, and a 60-second storage URL;
  - an audit event for every export, with its filters and row count;
  - the requester notified;
  - a migration with RLS.
  - Tests: a revoked right blocks the download; the link expires; the attack tests on the export rows. `// req: FR-RPT-09`
- [ ] 3.4 The accountant exports: the GL, TB, journals, the fixed asset schedule, and AR and AP detail (GST detail comes
  with `p1-gst`). `// req: FR-RPT-10`
- [ ] 3.5 `export-dialog` as designed: format, options, background progress, and the expired and blocked states.

## 4. Management reports (FR-RPT-03)

- [ ] 4.1 Sales by customer and item, spend by supplier, and the top expenses, as at a run of months (D6), with their
  screens in the reports index. `// req: FR-RPT-03`

## 5. Charts (FR-VIZ-01…08)

- [ ] 5.1 The palette validator in `make check` (D7): colour-vision separation and contrast in both themes.
  `// req: FR-VIZ-07`
- [ ] 5.2 The `Chart` component on ECharts, loaded on demand:
  - the form by the data's job, and one y-axis;
  - fixed entity colours, with Other past 8 series;
  - status colours only with an icon and a label;
  - tooltips, a legend and direct labels;
  - a table view, and PNG and data export.
  - Tests: Vitest with axe, and the table view's figures equal the chart's. `// req: FR-VIZ-01` to `// req: FR-VIZ-08`
- [ ] 5.3 First uses: the P&L's monthly trend and the top expenses.

## 6. Finish

- [ ] 6.1 The review register: the statement pages in an area awaiting the accountant (its own PR, which the co-founder
  approves).
- [ ] 6.2 `[live-verify]` An XLSX opened in Excel and Google Sheets, and a PDF printed, with the owner.
- [ ] 6.3 `make check`, `make test` and `make test-e2e`; archive.
