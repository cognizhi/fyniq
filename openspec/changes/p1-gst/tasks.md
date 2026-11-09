# Tasks

Each slice below is one pull request that passes `make test` on its own. Each carries its migration, its attack tests,
its API with authorisation-matrix rows, its screens with help keys and guide sections compiled into the knowledge base,
and the pinned or hand-worked tests behind every figure and date it shows. Load `fyniq-sg-statutory`,
`fyniq-feature-change` and `fyniq-design-system` before each slice; `fyniq-accounting-domain` for slice 2.

## 1. The decisions and the specs

- [x] 1.1 PRD-001 rev 0.208 records the owner's decisions of 7 Nov 2026:
  - FR-TAX-05: ⚠ at 80%, a setting [accountant to confirm]; the prospective test from a forecast recorded with its
    evidence;
  - FR-TAX-07: the Statutory calendar row of Appendix D, added with the build; the setting Pays staff.

  The main spec `tax/gst-and-filings` is updated and stamped (`python3 scripts/prdspec.py stale` is clean, `make check`
  passes). `// req: FR-TAX-05`, `// req: FR-TAX-07` (spec text)

## 2. The packs (slice 1)

- [ ] 2.1 `sg-gst@2026.3` (D1):
  - `2026.2`'s tables, plus the `registration_threshold` limit with the source `iras-register` (quote, URL, retrieved
    2026-11-07);
  - pinned limits for 2026-11-06 (none), 2026-11-07 and 2030-12-31.

  `go test ./internal/domain/pack` loads it, and its pinned cases pass. `// req: FR-TAX-05`, `// req: ENG-30`,
  `// req: QA-ENG-08`
- [ ] 2.2 `sg-deadlines@2026.1` (D1, D2):
  - `pack.Schemas` gains its tables (`rules`, `holidays`, `extensions`) and pinned tables (`due_dates`, `working_days`,
    `extensions`), with `checkDeadlines` for the columns' values;
  - `pack.DueOn` and `pack.NextWorkingDay`, with `RunPinned` running the three pinned tables;
  - the rows and their sources from IRAS (GST filing due dates, the myTax downtime extension, ECI, Form C-S, AIS,
    registration), CPF Board (due date), ACRA (annual return, AGM) and MOM (public holidays 2026 and 2027), each quoted.

  The pinned cases are listed in D2, worked from the sources' own examples and not from the rows.
  Tests: `TestDueOnFollowsMonthEnds`, `TestNextWorkingDaySkipsWeekendsAndHolidays`, `TestAYearWithoutHolidaysIsNotAssessed`,
  and the pack's pinned run. `// req: FR-TAX-07`, `// req: CMP-02`, `// req: ENG-30`, `// req: QA-ENG-08`
- [ ] 2.3 `migrate up` loads both versions; the global-pack golden files are updated. `fyniq-sg-statutory` and its PRD
  §14.2 snapshot name the new pack. `make check` and `make test`.

## 3. The registration monitor (slice 2)

- [ ] 3.1 `gst.TaxableSupplies` (D3):
  - invoices, credit notes and bank transactions;
  - Box 1 and Box 2 codes on revenue accounts, at base amounts;
  - reversals on their own day;
  - the unmatched line.

  Hand-worked fixtures:
  - an SR invoice and a ZR invoice in SGD;
  - a USD invoice at its rate;
  - an ES line and an OS line, both left out;
  - a credit note;
  - a void in the next year;
  - an asset disposal invoice, left out;
  - a bank receipt coded SR.

  `// req: FR-TAX-05`, `// req: QA-RPT-01`
- [ ] 3.2 The `gst_forecast` document (D4):
  - migration with the document type, FY298 guard, NOT VALID then validated;
  - draft, replace and record with evidence (GST-31 without it).

  Attack test: keys, amount and scale whoever writes. Audit events `document.created`, `document.revised` and
  `document.posted` asserted. `// req: FR-TAX-05`, `// req: AUD-01`
- [ ] 3.3 `gst.Monitor` (D4) and the setting `gst.threshold_warning_percent`. Tests:
  - each status: below, near at exactly 80%, liable by last year, liable by a forecast, registered, and Not assessed
    before the threshold's day;
  - the dates to apply by and to start, by D2.

  `// req: FR-TAX-05`
- [ ] 3.4 The API (D8): `getGSTMonitor`, the forecast operations and their evidence, under GST returns, with
  authorisation-matrix rows; `make openapi`; the error codes GST-30 and GST-31. `// req: FR-TAX-05`, `// req: QA-SEC-03`
- [ ] 3.5 C.1 #37 `gst.registration-threshold` (D9), with a fixture for each status, and the trigger on documents and the
  setting. `// req: C.1 #37`, `// req: QA-RPT-02`

## 4. The statutory calendar (slice 3)

- [ ] 4.1 `deadlines.Calendar` (D5, D7) and the settings `calendar.pays_staff`, `corporate.agm_route` and
  `corporate.overseas_branch_register`. Tests:
  - a December and a March year end;
  - quarterly and monthly GST;
  - Pays staff turned on mid-year;
  - each AGM route;
  - the overseas branch register;
  - no financial year (Not assessed);
  - an anchor before the rules' day;
  - the registers' items listed with their links.

  `// req: FR-TAX-07`, `// req: CMP-02`, `// req: FR-EQ-09`, `// req: FR-EQ-25`
- [ ] 4.2 The `statutory_filing` document (D6):
  - migration with the document type, FY299 guard, and the partial unique index on a live record's key (built
    concurrently, in its own migration);
  - record and withdraw;
  - evidence;
  - the setting `evidence.statutory_filing`.

  Attack tests: a second live record of a key, an unknown key form, a filed outcome without its date. Audit events.
  `// req: FR-TAX-07`, `// req: FR-DOC-06`, `// req: AUD-01`
- [ ] 4.3 Appendix D's Statutory calendar row (D8):
  - in the PRD (rev 0.2xx), `access/roles-and-segregation` (stamped) and `authz.Grants`, in one commit;
  - the API's `getStatutoryCalendar`, `recordStatutoryFiling`, `withdrawStatutoryFiling` and the evidence operations,
    with matrix rows;
  - CAL-01 to CAL-04;
  - `make openapi`.

  `// req: FR-TAX-07`, `// req: QA-SEC-03`
- [ ] 4.4 C.2 #13 `gst.return-due` and C.2 #15 `compliance.deadlines` (D9), each with fixtures for OK, ⏰ due soon,
  ⏰ overdue and Not assessed. `// req: C.2 #13`, `// req: C.2 #15`, `// req: CMP-02`, `// req: QA-RPT-02`
- [ ] 4.5 The daily `notify.deadlines` job (D10):
  - PlatformSchedules at 07:35 SGT;
  - `deadline.reminder` at 30, 14, 7 and 1 days, and `deadline.overdue` once;
  - keyed so a rerun adds nothing;
  - the audience by the row.

  The C.2 #12 schedule check includes it. `// req: FR-TAX-07`, `// req: CMP-02`, `// req: FR-NTF-01`

## 5. The screens (slice 4)

- [ ] 5.1 GstReturns' monitor (D12, `/compliance/gst`):
  - the card, the forecasts and their form with evidence;
  - the not-registered note;
  - the FR-UX-41 banner (the review register names the page).

  Registry tags (Expert), help keys, and tests: Vitest with axe; every figure from the server. `// req: FR-TAX-05`,
  `// req: QA-UI-05`
- [ ] 5.2 StatutoryCalendar (D12, `/compliance/calendar`):
  - the month and list views;
  - the area filter;
  - record as filed, or not required, with evidence;
  - withdraw;
  - Not assessed first.

  Registry tags (Basic and Expert), help keys, and tests, including mode parity (QA-UI-04). `// req: FR-TAX-07`,
  `// req: CMP-02`, `// req: QA-UI-04`
- [ ] 5.3 The Inbox's Deadlines section and the Compliance navigation entries. `// req: FR-TAX-07`
- [ ] 5.4 Guide pages `docs/user-guide/statutory-calendar.md` and `docs/user-guide/gst-registration.md`, plus the new
  settings in `organisation-settings.md`. Compiled into `kb/pages/`, then `make kb-index`, `make kb-lint` and a log entry.
  `// req: QA-UI-05`

## 6. As built (slice 5)

- [ ] 6.1 The compliance canvas's StatutoryCalendar and GstReturns fragments, and the inventory and sample data, match
  what was built. `build.py check`, then publish the canvas (fyniq-openspec `references/design.md`). The threat model
  and the security checklist are updated: personal data in evidence; no AI or MCP write.
- [ ] 6.2 `make test` (the authorisation matrix in full), `make check`, `make up` starts. `make roadmap` once the tasks
  above are ticked.

## 7. At GST registration (question 2), not before

- [ ] 7.1 FR-TAX-01: the reverse-charge codes and the setting (question 29, the tax agent's decision).
- [ ] 7.2 FR-TAX-02 and FR-TAX-03: the F5 return with every box, the roll-forward to the GST control (INV-07) and the
  filing record and lock. These complete the calendar's F5 items, and `recordStatutoryFiling` then refuses an F5 key.
  Screen `gst-return`.
- [ ] 7.3 FR-TAX-04: the IRAS audit listings.
- [ ] 7.4 FR-TAX-12, C.2 #46 and C.2 #47: the adjustment queue. Screen `gst-adjustments`.

## Workflow follow-up

- Archive with `--skip-specs` once 2 to 6 are built, leaving 7 to a successor change at registration
  (`/opsx:archive`, `prdspec.py coverage`, stamp).
