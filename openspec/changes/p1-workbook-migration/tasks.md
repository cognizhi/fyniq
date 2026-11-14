# Tasks

Each slice is its own pull request, stacked in order; each passes `make check` and `make test` on its own.

## 1. Reading the workbook and the validation report (FR-MIG-01, M1)

- [x] 1.1 `internal/sheet` reads named sheets with a workbook's bounds (design D1): shared and inline strings, each
  cell's cached value as exact decimal text, columns found by their header on row 5. A workbook saved without its
  calculated values is refused with how to fix it. Tests: the fixture's sheets read; a bomb over each bound stops at it;
  a sheet without cached values is refused. `// req: FR-MIG-01`
- [x] 1.2 `scripts/workbook_migration_fixture.py` writes `test/migration/workbook-sample.xlsx` from a pinned commit of
  `zh-sheet-accounting`: the sheets the import reads, values only, with the commit recorded (design D12). Verified by
  `TestTheFixtureIsTheRecordedOne`, which checks the file against the hash and commit recorded beside it.
- [x] 1.3 The PRD names `RND-IMPORT` in §10.4 (the workbook's calculated figures rounded half away from zero to cents,
  once, after every saved number is read to a spreadsheet's 15 significant digits), with a PRD revision (rev 0.255). `internal/domain/rounding` gains it, with its pinned cases and the oracle's
  implementation. Tests `// req: RND-IMPORT` in Go and in the oracle.
- [x] 1.4 `internal/migration`'s typed model of every sheet the import reads (design D2, D5): dates from serial numbers,
  codes from "code - name" labels, amounts exact; a row the workbook blocked (✖) is reported and kept out. Test
  `// req: FR-MIG-01` reads the fixture into the model and compares its counts and totals with the sheets' own.
- [x] 1.5 The migration record: migrations for `migration_run`, `migration_step` and `migration_file`
  (organisation-keyed, forced row-level security, every change audited on the run; 000325, FY325). Appendix D gains the
  row "Workbook import (migration)" in the PRD and in `authz.Rows` together (Owner A; Controller and Accountant C; Auditor
  V; AI none). `POST /v1/migrations` is refused for an organisation with a posted entry (MIGRATION-09, rev 0.254; the
  database refuses it too) and answers the organisation's one migration once started.
  `POST /v1/migrations/{id}/workbook` (scanned, hashed, kept as M1's evidence, marked payroll) and
  `GET /v1/migrations/{id}/report`, validating in the background (`migration.validate`, an operation) inside a
  transaction that rolls back. The Checks export is made from the uploaded workbook's own Checks sheet and kept beside
  it; a check showing ✖ is an error (MIGRATION-08, §17.1 M1). As built, the validation checks the organisation (no
  posted entry, the workbook's base currency); the mapping's and each command's previews join the same rolled-back
  transaction as tasks 2.1 and 4.2–4.6 build them. Tests `TestAWorkbookImportGoesOnlyIntoAnOrganisationWithNoPostings`,
  `TestTheValidationReportCommitsNothing` (`// req: FR-MIG-01`, `// req: data/migration Workbook freeze (M1)`), the
  matrix's rows, `TestAttackTheMigrationRecordKeepsItsRules` (another organisation sees none of it).

## 2. Mapping (M2)

- [x] 2.1 The mapping (design D4): accounts by code onto the organisation's chart, or the standard chart a new
  organisation loads (the workbook's 7040 onto Fyniq's 7050), creating those Fyniq lacks from the Accounts sheet; GST
  codes onto `sg-gst`'s; departments and projects as dimension values (rev 0.254); budget lines onto their report group's
  first account; bank rules in order. Each valid workbook proposes the mapping's next version, keeping the choices of the
  one before; what it gets wrong is a warning in the report and an error on the mapping. `GET`/`PATCH
  /v1/migrations/{id}/mapping` (each change the next version) and `POST .../mapping/sign-off`: the Accountant alone,
  stepped up, over the version's hash, under Appendix D's new row "Workbook import sign-offs" (rev 0.256); a version is
  signed once, and one of a workbook that is not the latest valid upload is MIGRATION-12 (000327, FY326). Tests
  `TestTheSampleMapsOntoANewOrganisation`, `TestTheMappingIsSignedByTheAccountantAlone` (a changed mapping after sign-off
  needs a new sign-off), `TestAttackTheMigrationMappingKeepsItsRules` (`// req: data/migration Set-up import and mapping
  review (M2)`), the matrix's rows.

## 3. Opening open items (FR-GL-06) [accountant to confirm]

- [x] 3.1 Invoices and bills flagged `opening_item` post at the books start under `opening_invoice@1` and
  `opening_bill@1` (rev 0.257; two rule versions where the plan named one, as each keeps a replay golden of its own)
  against the new system account 3900 "Opening balance clearing" (design D6; standard chart, role
  `opening-balance-clearing`, 000328), in their currency at their rate, with no revenue, cost or GST; the opening
  balances carry receivables and payables on the clearing account. Every identity, settlement, ageing and replay reads
  the item's rule. The oracle implements the rule independently, and the differential gains the source `opening-items`.
  Tests `TestAnOpeningItemPostsItsTotalAgainstTheClearingAccount`, `TestOpeningItemsPostAtTheBooksStartAndSettleAfterIt`
  (`// req: FR-GL-06`: an opening USD invoice paid after the books start; `// req: INV-07`: the sub-ledgers agree at the
  books start; every entry replays), the oracle's `test_opening_items.py`, `make differential`.
- [x] 3.2 C.2 #63: 3900 Opening balance clearing not at nothing (⚠) once the opening balances and items have posted,
  with a fixture that triggers it. `// req: FR-GL-06`

## 4. The commit (M3)

- [x] 4.1 The commit as one approved request (design D3, rev 0.258): prepared under C on "Workbook import (migration)",
  approved by an Owner who did not prepare it (FM-1), then run in batches of at most 50 records on the API's role, each
  record in its own transaction with its business key, its workbook origin and the approval, resumable, and the
  approval the import's one entry in the weekly review. `POST /v1/migrations/{id}/commit` with the founders' choice
  (`history` or `clean_start`), `POST /v1/migrations/{id}/commit:run`, `GET /v1/migrations/{id}/commit`. Tests
  `TestACommitResumesWhereItStopped`, `TestEveryImportedRecordNamesItsRow` (`// req: FR-MIG-01`).
- [x] 4.2 The set-up: profile and settings, dimensions, chart, contacts, items, FX rates, bank rules, money accounts.
  Test `// req: data/migration Set-up import and mapping review (M2)` on the fixture. (As built: the profile with the
  first financial year's missing months; the GST registration from the books start, quarterly [accountant to confirm];
  the standard chart for an organisation without one, then the accounts the mapping creates, money accounts among them
  in the base currency; a UEN not in ACRA's form and a bank rule to a money account are warnings, left out.)
- [x] 4.3 History of the current financial year (design D5): opening balances with opening items; invoices with Doc
  Lines and spreads; bills; quotes and orders; Transactions as receipts, bill payments or categorised bank
  transactions; journals; assets with past depreciation. Test `// req: data/migration History or opening balances
  import (M3)` (history) on the fixture. (Part built in slice 5: the opening balances with the opening items, invoices with Doc Lines,
  spreads and credit notes, bills and journals, their numbers kept, rev 0.259; slice 6: Transactions as statement
  lines, receipts, payments, transfers and categorisations, rev 0.260; slice 7: quotes and orders, assets with their
  depreciation. Rows to share capital or a SAFE wait for task 4.6.)
- [ ] 4.4 Payroll as one summary journal a month (design D7, rev 0.254), approved under Journals. Test
  `// req: FR-MIG-02` "Imported pay runs keep the amounts paid" on the fixture's months.
- [ ] 4.5 The clean start from Carry Forward: balances and open items at the cut-over. Test
  `// req: data/migration History or opening balances import (M3)` (clean start) on the fixture.
- [ ] 4.6 Both paths: active Recurring rows from the cut-over on; Budget as an approved version; the Forecast drivers
  as a forecast document; Funding as the equity register's holdings [accountant to confirm]; Timesheets and per-person
  Employees rows reported as kept in the archive. Tests on the fixture, `// req: FR-MIG-01`.

## 5. Reconciliation (M4)

- [ ] 5.1 Parity (design D8): each account's opening, monthly movement and month-end closing against the workbook's
  Trial Balance sheet, to the cent after `RND-IMPORT`; the register with proposed classes the Accountant decides; the
  workbook's Checks rows against Fyniq's C.1 checks; the parity report and register signed together (the Accountant,
  step-up, over their hash). `GET /v1/migrations/{id}/reconciliation`, `PATCH .../register/{line}`,
  `POST .../reconciliation/sign-off`. Tests `// req: FR-MIG-02`, `// req: QA-MIG-01`, `// req: G4-016` (the open USD
  receivables' revaluation as class b), `// req: data/migration Parity check (M4)`.

## 6. The parallel month and the archive (M5, M6)

- [ ] 6.1 M5: the workbook at the end of the parallel month uploaded and that month reconciled the same way, then
  signed. M6: the frozen workbook archived as evidence under retention, its hash recorded, the record closed and read
  only. Tests `// req: data/migration Parallel run (M5)`, `// req: data/migration Go-live and workbook archive (M6)`.

## 7. Generic import (FR-MIG-03)

- [ ] 7.1 A template per entity, each laid out as the workbook's sheet for it (design D9), as CSV and XLSX:
  `GET /v1/imports/templates/{entity}`, and `POST /v1/imports/{entity}` with `Prefer: return=preview` for the dry run.
  Each record goes through its command with its normal approvals, in any organisation, needing the entity's own C.
  Tests `// req: FR-MIG-03` for each entity (a dry run commits nothing; a commit equals the same rows entered by hand),
  the matrix's rows.

## 8. Full tenant export (FR-MIG-04)

- [ ] 8.1 The approval object `tenant_export` (Appendix D's row: the Owner and a second approver) and the worker that
  writes the ZIP (design D10): CSV per table, documents as JSON, files, the audit trail with its hashes, the manifest
  with SHA-256s, the README. The table catalogue's test fails on a table with `organisation_id` that is neither exported
  nor excluded. Downloaded by the requester for 24 hours, recorded as a sensitive export (AUD-05). Sealed values in
  clear [counsel to confirm]. Tests `// req: FR-MIG-04` (the manifest's hashes match the files; a second approver is
  needed; another person cannot download it), `// req: AUD-05`.

## 9. Screens, help and the guide

- [ ] 9.1 `workbook-import` (Settings › Import, Expert): the steps M1 to M6 with their evidence and sign-offs, the
  validation report by sheet, the mapping, the commit's progress (the operations tray), the parity report and register,
  the generic import's templates and dry run. Built from `docs/design/screens/src/WorkbookImport.html`; help keys in the
  registry; guide `docs/user-guide/workbook-import.md` compiled into `kb/`. Tests `workbook-import.test.tsx` (each step's
  states, axe) `// req: FR-MIG-01`, `// req: FR-MIG-02`, `// req: FR-MIG-03`.
- [ ] 9.2 `data-export` (Settings › Data): the data region and retention (read only where fixed) and the tenant export,
  from asking to downloading. Built from `docs/design/screens/src/DataExport.html`; help keys; guide
  `docs/user-guide/data-export.md` compiled into `kb/`. Tests `data-export.test.tsx` `// req: FR-MIG-04`.

## 10. The rehearsal and the finish

- [ ] 10.1 The rehearsal: one test runs M1 to M6 on the sample workbook in the local flavour and ends with every account
  and month at parity, the register only the fixture's expected class a and b lines, and the Checks comparison as
  expected. `// req: QA-MIG-01`, `// req: data/migration Parity check (M4)`.
- [ ] 10.2 `docs/security/threat-models/migration-and-export.md` (design D13); the build plan's paragraph on what was
  built; `make check`, `make test`, `make test-e2e` and `make differential` pass.

## Workflow follow-up

- `/opsx:verify p1-workbook-migration`, then `/opsx:archive p1-workbook-migration --skip-specs`.
- `p1-launch-readiness` follows (wave 7): the founders' own migration runs this path on the real workbook.
