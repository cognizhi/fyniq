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

**Left in this change (21 Nov 2026):** 2.3 and 2.4 (the working paper's store, API, matrix rows and the screen `filing-packs`), 3.2 and 3.3 (the claim and its posting with the oracle's half and a differential scenario, and the screen `bad-debt-relief`), 4.1's tasks (`pack.update_available` and `pack.correction_assessment`), and 5.1 (the IRAS filing port and stub: the payloads of IRAS's GST (F5, F8, F7) and Form C-S APIs are behind IRAS's developer sign-in, so no layout is guessed: `[live-verify]`). Why: each needs a store, an API and a screen of its own and the posting leg needs the engine and the oracle together, which a slice of its own does.
