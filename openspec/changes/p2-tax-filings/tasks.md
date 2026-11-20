# Tasks

## 1. Decisions and specs
- [ ] 1.1 PRD-001 rev 0.480 records the decisions; the specs are updated and stamped. `// req: FR-TAX-06`, `// req: FR-AR-11`, `// req: ENG-33`

## 2. Corporate income tax
- [ ] 2.1 `cit.Paper` (D1) with hand-worked cases from IRAS's examples. `// req: FR-TAX-06`
- [ ] 2.2 The form selector and the ECI and filing deadlines (D2). `// req: FR-TAX-06`
- [ ] 2.3 Store, API, matrix rows. `// req: FR-TAX-06`, `// req: QA-SEC-03`
- [ ] 2.4 Screen `filing-packs`. `// req: QA-UI-05`

## 3. Bad-debt relief
- [ ] 3.1 The eligibility dates (D3), pinned from IRAS's rule. `// req: FR-AR-11`
- [ ] 3.2 The claim and its posting, with the oracle's half and the differential scenario. `// req: FR-AR-11`, `// req: ENG-04`
- [ ] 3.3 Screen `bad-debt-relief`. `// req: QA-UI-05`

## 4. Pack updates
- [ ] 4.1 `pack.update_available` with its diff and `pack.correction_assessment` (D4). `// req: ENG-33`

## 5. The IRAS filing port
- [ ] 5.1 `ports.IRASFiling`, the stub and the idempotent record (D5). **[live-verify]** the payload mapping against IRAS's sandbox. `// req: FR-TAX-03`
