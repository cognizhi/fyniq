# Tasks

Each slice carries its migration (001100 to 001149), port and adapter, attack tests, API with matrix rows, screen with help keys and
guide pages compiled into the knowledge base, and the oracle's half where it posts. Load `fyniq-feature-change` and
`fyniq-design-system` before each slice.

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.540 records the decisions; the specs are updated and stamped. `// req: FR-RPT-04`, `// req: FR-RPT-06`, `// req: FR-EQ-23`
  (23 Nov 2026: rev 0.540, then 0.546 for the Appendix D row the builder needed.)

## 2. Report builder and schedules
- [x] 2.1 `reportdef` domain: validation, the formula evaluator and the run (D1), with hand-worked cases. `// req: FR-RPT-04`
  Built: `internal/domain/reportdef` (the grammar, `Validate` RDEF-01 to RDEF-08, `Compute`, `NextRun`) and `financials.AccountFigures`; calculated rows round once under RND-KPI (design D1 said RND-PERCENT: percent rows use RND-KPI at 1 decimal, the same rounding). Tests: `TestAReportIsWorkedOutFromTheFiguresByHand`, `TestACalculatedRowRoundsOnceHalfAwayFromZeroAndNeverDividesByZero`.
- [x] 2.2 Store, API (definitions, run, share), matrix rows (D2). `// req: FR-RPT-04`, `// req: QA-SEC-03`
  Built: migration 001100, `ReportDefinitions`, the seven operations and the Appendix D row `Report definitions and schedules` (rev 0.546: the Reports row is view-only and the AI holds V on it, so saving needed a row of its own). Tests: `TestAReportIsSavedRunAndChangedThroughTheAPI`, `TestOnlyReadersOfTheWholeLedgerUseTheBuilder`, the matrix rows (`FYNIQ_MATRIX_ONLY='ReportDefinition|ReportDraft|ReportSchedule'`), `TestAReportDefinitionAndItsScheduleKeepTheirRulesInTheDatabase`.
- [x] 2.3 Schedules, the daily job and the send-time permission check (D3). Slack is a fake: **[live-verify]**. `// req: FR-RPT-07`
  Built: `report_schedule`, `report_schedule_run`, the hourly job `reports.send` (`internal/reportsend`) and the notification `reports.scheduled` (title with the report's name and month, no figure: stricter than D3's option, so no setting). **Left:** the Slack post (`[live-verify]`; a Slack run records every recipient as skipped with that reason). Test: `TestADueReportIsSentOnlyToThoseWhoStillMayReadIt`.
- [x] 2.4 Screen `report-builder`, help, guide and kb. `// req: QA-UI-05`
  Built: `ReportBuilder.tsx` (Expert), help keys `reports.builder` and `report.schedule`, the guide `report-builder.md` and its kb page. Test: `reportBuilder.test.tsx`.

## 3. Board pack, investor updates and access
- [x] 3.1 Board pack store, API, publish and PDF (D4, D5). `// req: FR-RPT-06`
  Built: migration 001101, `BoardPacks`, listBoardPacks, getBoardPack, createBoardPackDraft, editBoardPackDraft, publishBoardPack (BPACK-01 to BPACK-05); figures frozen at publication with the ledger revision; a month that is not closed is published and says so. Tests: `TestABoardPackIsFrozenWhenPublishedAndReadAfterSignIn`, `TestABoardPackIsFrozenWhenPublishedAndAnUpdateGoesOnceToItsRecipients`. **Left:** the PDF render of a published pack (the render service) and an approval step when the organisation has dual control.
- [ ] 3.2 Investor access (D7). `// req: FR-EQ-24`
  Built in part: a Board viewer reads published packs, the updates sent to them and, through `getMyHolding`, their own holding (the portal). **Left:** the Owner's grant of the full cap table (a new operation beside getCapTable, with a reason and a revoke): it widens a Board viewer's read of the cap table, so it waits for the owner (final report).
- [x] 3.3 Investor updates, read status, archive (D6). `// req: FR-EQ-23`
  Built: `investor_update` and `investor_update_read` (001101), five operations, the notification `investor.update` with no figure; the list is the archive. Test: `TestAnInvestorUpdateGoesOnceToThoseItIsSentTo`.
- [ ] 3.4 Obligation tasks (D8). `// req: FR-EQ-28`
- [x] 3.5 Screens `board-pack`, `investor-updates`, `board-portal`, help, guide and kb. `// req: QA-UI-05`
  Built: `BoardPack.tsx`, `InvestorUpdates.tsx`, `BoardPortal.tsx` and `PackFigures.tsx`, three guide pages and their kb pages. Test: `boardPack.test.tsx`.

## 4. Dashboards and KPIs
- [x] 4.1 Layouts and tiles (D9). `// req: FR-VIZ-09`, `// req: FR-KPI-05`
  Built: migration 001103, `domain/dashboard`, five operations (DASH-01 to DASH-04). Tests: `TestALayoutNamesCatalogueTilesOnceInsideTheGrid`, `TestADashboardIsTheTilesAPersonOrARoleArranged`. The tiles are the KPIs; chart tiles are **Left**.
- [ ] 4.2 Customer count port (D10). `// req: FR-KPI-04`
- [x] 4.3 Anomaly alerts (D10). `// req: FR-KPI-06`
  Built: migration 001102, `domain/anomaly` (exact, no square root), `internal/anomalyjob`, the job `kpi.anomalies`, listKpiAlerts. Tests: `TestThreeStandardDeviationsAboveTheTrendIsAnAnomalyAndJustBelowIsNot`, `TestASpendAnomalyIsRaisedOnceForTheClosedMonth`.
- [ ] 4.4 Benchmark packs and the comparison (D10), on a synthetic fixture. `// req: FR-KPI-07`
- [x] 4.5 Screen `dashboards`, help, guide and kb. `// req: QA-UI-05`
  Built: `Dashboards.tsx` (a person's tiles added, reordered, resized and saved), the guide and its kb page. Test: `dashboards.test.tsx`.

## 5. Reports of deferrals and assets
- [ ] 5.1 Deferral roll-forward (D11). `// req: FR-RPT-13`
  Built in part: `journal.DeferralRollForward` and getDeferralRollForward (closing = opening + added - released + reversed, per customer and supplier, tied to the ledger). Test: `TestTheDeferralRollForwardThroughTheAPI`. **Left:** billed, collected and recognised revenue side by side per customer, and the screen.
- [ ] 5.2 Asset schedule and the capital allowance pack (D11). `// req: FR-FA-04`

## 6. Planning
- [ ] 6.1 Rolling forecast and its accuracy (D12). `// req: FR-PLN-04`
- [ ] 6.2 Funding plan (D12). `// req: FR-PLN-07`

## 7. The Auditor's engagement
- [ ] 7.1 Engagements, requests, samples (D13). `// req: FR-UX-42`

## 8. Exit waterfall, allocations, shares for services
- [ ] 8.1 The waterfall and its oracle half (D14). `// req: FR-EQ-29`, `// req: QA-ENG-02`
  Built in part: `equity.ModelWaterfall` (non-participating, uncapped participating, seniority and pari passu, the as-converted split, each class's better choice, a capped preference refused by name), hand-pinned cases and the oracle's half (`oracle/src/oracle/waterfall.py`, every set of conversions tried) agreeing on 60 generated structures (`test/differential/waterfalls/cases.json`). Tests: `TestAnExitPaysTheNonParticipatingPreferenceOrTheConversionWhicheverIsMore`, `TestTheWaterfallAgreesWithTheOracleToTheCent`, `test_waterfall.py`. **Left:** the sandbox operation that reads the register's classes, holdings and money invested, and the screen `exit-waterfall`.
- [ ] 8.2 Allocation rules with the oracle's half and a differential scenario (D15). `// req: FR-DIM-03`, `// req: ENG-04`
- [ ] 8.3 Shares for services with the oracle's half and a differential scenario (D15). `// req: FR-EQ-13`, `// req: ENG-04`
