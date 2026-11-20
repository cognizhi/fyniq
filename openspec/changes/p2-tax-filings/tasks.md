# Tasks

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.480 records the decisions; the specs are updated and stamped. `// req: FR-TAX-06`, `// req: FR-AR-11`, `// req: ENG-33`
  (21 Nov 2026, rev 0.480 and 0.513.)

## 2. Corporate income tax
- [x] 2.1 `cit.Paper` (D1) with hand-worked cases from IRAS's examples. `// req: FR-TAX-06`
  (Built: `internal/domain/taxfilings` `Work`, five hand-worked cases, a loss, the single rounding and each refusal. The tax is rounded once, half away from zero `[accountant to confirm]`.)

- [x] 2.2 The form selector and the ECI and filing deadlines (D2). `// req: FR-TAX-06`
  (Built: `pack.FilingForm` and `sg-tax-filings@2026.1`'s limits; the deadlines are `sg-deadlines`'s. The thresholds are quoted from IRAS's overview page as a search returned its text on 20 Nov 2026: read the page again at the next review.)
- [ ] 2.3 Store, API, matrix rows. `// req: FR-TAX-06`, `// req: QA-SEC-03`
- [ ] 2.4 Screen `filing-packs`. `// req: QA-UI-05`

## 3. Bad-debt relief
- [x] 3.1 The eligibility dates (D3), pinned from IRAS's rule. `// req: FR-AR-11`
  (Built: `pack.BadDebtRelief` and the pack `sg-tax-filings@2026.1`, ten pinned cases and five more by hand. The last day to claim is the day before the anniversary `[accountant to confirm]`.)

- [ ] 3.2 The claim and its posting, with the oracle's half and the differential scenario. `// req: FR-AR-11`, `// req: ENG-04`
- [ ] 3.3 Screen `bad-debt-relief`. `// req: QA-UI-05`

## 4. Pack updates
- [ ] 4.1 `pack.update_available` with its diff and `pack.correction_assessment` (D4). `// req: ENG-33`
  (Built in part: `pack.Diff`. **Left:** the tasks.)

## 5. The IRAS filing port
- [ ] 5.1 `ports.IRASFiling`, the stub and the idempotent record (D5). **[live-verify]** the payload mapping against IRAS's sandbox. `// req: FR-TAX-03`

(21 Nov 2026, second pass: Built: `workTaxPaper` (the paper from the ledger; `TestTheTaxWorkingPaperIsWorkedFromTheLedger`) and the page Compliance > Year-end tax (2.3 in part, 2.4 in part: the page is at `/compliance/filing-packs`, without the design's Year of Assessment in the path); `getBadDebtRelief` and the page Sales > Bad debts (3.1 queue, 3.3 in part); `listStatutoryPackUpdates` with its page section (4.1 in part); `ports.IRASFiler`, `local.IRASFiler` and `contracts/connectors/iras-filing.contract.json` (5.1: no caller until `p1-gst` 7.2, the payloads `[live-verify]`).)

**Left in this change:** the working paper's store with its adjustments' evidence and the ECI and Form C-S data pack (2.3, 2.4), the relief claim and its posting leg with the oracle's half and a differential scenario (3.2: it needs the engine, the oracle and the differential together), the insolvency date of a debtor, the stored `pack.update_available` and `pack.correction_assessment` tasks (4.1), and the IRAS adapter and its payload mapping (5.1, `[live-verify]`).
