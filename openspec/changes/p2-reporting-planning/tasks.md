# Tasks

Each slice carries its migration (001100 to 001149), port and adapter, attack tests, API with matrix rows, screen with help keys and
guide pages compiled into the knowledge base, and the oracle's half where it posts. Load `fyniq-feature-change` and
`fyniq-design-system` before each slice.

## 1. Decisions and specs
- [ ] 1.1 PRD-001 rev 0.540 records the decisions; the specs are updated and stamped. `// req: FR-RPT-04`, `// req: FR-RPT-06`, `// req: FR-EQ-23`

## 2. Report builder and schedules
- [ ] 2.1 `reportdef` domain: validation, the formula evaluator and the run (D1), with hand-worked cases. `// req: FR-RPT-04`
- [ ] 2.2 Store, API (definitions, run, share), matrix rows (D2). `// req: FR-RPT-04`, `// req: QA-SEC-03`
- [ ] 2.3 Schedules, the daily job and the send-time permission check (D3). Slack is a fake: **[live-verify]**. `// req: FR-RPT-07`
- [ ] 2.4 Screen `report-builder`, help, guide and kb. `// req: QA-UI-05`

## 3. Board pack, investor updates and access
- [ ] 3.1 Board pack store, API, publish and PDF (D4, D5). `// req: FR-RPT-06`
- [ ] 3.2 Investor access (D7). `// req: FR-EQ-24`
- [ ] 3.3 Investor updates, read status, archive (D6). `// req: FR-EQ-23`
- [ ] 3.4 Obligation tasks (D8). `// req: FR-EQ-28`
- [ ] 3.5 Screens `board-pack`, `investor-updates`, `board-portal`, help, guide and kb. `// req: QA-UI-05`

## 4. Dashboards and KPIs
- [ ] 4.1 Layouts and tiles (D9). `// req: FR-VIZ-09`, `// req: FR-KPI-05`
- [ ] 4.2 Customer count port (D10). `// req: FR-KPI-04`
- [ ] 4.3 Anomaly alerts (D10). `// req: FR-KPI-06`
- [ ] 4.4 Benchmark packs and the comparison (D10), on a synthetic fixture. `// req: FR-KPI-07`
- [ ] 4.5 Screen `dashboards`, help, guide and kb. `// req: QA-UI-05`

## 5. Reports of deferrals and assets
- [ ] 5.1 Deferral roll-forward (D11). `// req: FR-RPT-13`
- [ ] 5.2 Asset schedule and the capital allowance pack (D11). `// req: FR-FA-04`

## 6. Planning
- [ ] 6.1 Rolling forecast and its accuracy (D12). `// req: FR-PLN-04`
- [ ] 6.2 Funding plan (D12). `// req: FR-PLN-07`

## 7. The Auditor's engagement
- [ ] 7.1 Engagements, requests, samples (D13). `// req: FR-UX-42`

## 8. Exit waterfall, allocations, shares for services
- [ ] 8.1 The waterfall and its oracle half (D14). `// req: FR-EQ-29`, `// req: QA-ENG-02`
- [ ] 8.2 Allocation rules with the oracle's half and a differential scenario (D15). `// req: FR-DIM-03`, `// req: ENG-04`
- [ ] 8.3 Shares for services with the oracle's half and a differential scenario (D15). `// req: FR-EQ-13`, `// req: ENG-04`
