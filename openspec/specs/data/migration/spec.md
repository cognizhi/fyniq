# data/migration Specification

## Purpose
How the books move from the `zh-sheet-accounting` workbook into Fyniq and how data leaves it again: the workbook import, reconciliation with the migration difference register, generic CSV/XLSX imports, the full tenant export, and the six-step migration path with the evidence each step leaves. Derived from PRD-001 §8.19 and PRD-001 §17.1.

## Requirements

### Requirement: FR-MIG-01 Workbook import
The system SHALL import the published `.xlsx` workbook, with a validation report before commit.

Source: PRD-001 §8.19 · Priority M · Phase P1

#### Scenario: What is imported
- **WHEN** the published workbook is imported
- **THEN** Settings, Accounts (with opening balances), Contacts, Employees, Items and Lists (FX rates, bank rules) are imported
- **AND** every record table is imported: Transactions, Invoices, Doc Lines, Bills, Journal, Payroll, Assets, Quotes & Orders, Timesheets, Recurring, Budget, Forecast drivers and Funding

#### Scenario: Validation report before commit
- **WHEN** a workbook import is run
- **THEN** a validation report is shown before anything is committed

### Requirement: FR-MIG-02 Import reconciliation
The imported trial balance SHALL equal the workbook's trial balance for every account and month to the cent, except the items in a migration difference register that the accountant signs off.

Source: PRD-001 §8.19 · Priority M · Phase P1

#### Scenario: Trial balance matches to the cent
- **WHEN** an import is reconciled
- **THEN** the imported trial balance equals the workbook's for every account and month to the cent, apart from register items

#### Scenario: The migration difference register
- **WHEN** a difference is entered in the register
- **THEN** it is listed by account and month with its class: (a) the cent allocation of spreads and depreciation, re-derived exactly by applying `ALLOC-EVEN` to the workbook's schedules; (b) deliberate rule changes (CPF graduated and PR rates, the AW ceiling and SHG tables, Appendix B.3; month-end FX revaluation, FR-GL-04; parameters effective-dated by period, §10.6); (c) workbook defects found during migration
- **AND** the accountant signs off the register

#### Scenario: Imported pay runs keep the amounts paid
- **WHEN** a pay run is imported
- **THEN** it keeps the amounts actually paid and reported to the CPF Board
- **AND** where the platform's calculation differs, the difference is a register item for the accountant's decision and is never posted silently

#### Scenario: Checks status is reproduced
- **WHEN** the import is complete
- **THEN** the workbook's Checks status is reproduced, except where a register item explains a difference

#### Scenario: G4-016 Import of a year with open USD receivables
- **WHEN** a year with open USD receivables at month ends is imported
- **THEN** the revaluation postings (or their suppression before cut-over, as decided) appear in the migration difference register by account and month
- **AND** the trial balance otherwise matches to the cent

### Requirement: FR-MIG-03 Generic import
The system SHALL provide a generic CSV/XLSX import for each entity, with templates and a dry run.

Source: PRD-001 §8.19 · Priority M · Phase P1

#### Scenario: Template per entity
- **WHEN** a user prepares a CSV or XLSX import for an entity
- **THEN** a template for that entity is available

#### Scenario: Dry run
- **WHEN** a user runs the import as a dry run
- **THEN** the result is shown without anything being committed

### Requirement: FR-MIG-04 Full tenant export
The Owner SHALL be able to export all of the organisation's data and documents in open formats (CSV/JSON plus files), with a manifest and hashes, under dual control.

Source: PRD-001 §8.19 · Priority M · Phase P1

#### Scenario: Export is dual-controlled
- **WHEN** the Owner starts a full tenant export
- **THEN** it needs dual control

#### Scenario: Contents of the export
- **WHEN** a full tenant export completes
- **THEN** it holds all data and documents as CSV/JSON plus files
- **AND** a manifest lists them with their hashes

#### Scenario: Uses of the export
- **WHEN** the organisation exits, answers an access request or meets a data-portability obligation
- **THEN** the full tenant export serves that purpose

### Requirement: Workbook freeze (M1)
The migration SHALL start by freezing the workbook at a month end, running `make verify` on the live file and exporting its Checks status.

Source: PRD-001 §17.1

#### Scenario: Evidence of a clean freeze
- **WHEN** the workbook is frozen at a month end and `make verify` has run on the live file
- **THEN** the exported Checks status shows zero blocking errors

### Requirement: Set-up import and mapping review (M2)
The migration SHALL import the set-up (Settings, Accounts, Contacts, Employees, Items, Lists) and review its mappings.

Source: PRD-001 §17.1

#### Scenario: Mappings reviewed and signed off
- **WHEN** the set-up is imported
- **THEN** the mappings of accounts to the platform chart of accounts, GST codes, departments and bank rules are reviewed
- **AND** the mapping report is signed off

### Requirement: History or opening balances import (M3)
The migration SHALL import history (FR-MIG-01) for the current financial year, or opening balances and open items at the cut-over date if the founders prefer a clean start.

Source: PRD-001 §17.1

#### Scenario: Full history for the current year
- **WHEN** the founders choose to import history
- **THEN** the current financial year's history is imported
- **AND** an import validation report is kept

#### Scenario: Clean start
- **WHEN** the founders prefer a clean start
- **THEN** opening balances and open items at the cut-over date are imported
- **AND** an import validation report is kept

### Requirement: Parity check (M4)
The migration SHALL check parity (FR-MIG-02, QA-MIG-01) of the trial balance by account and month, the AR and AP open items, the GST F5 for the last return, and payroll totals.

Source: PRD-001 §17.1

#### Scenario: Parity evidence
- **WHEN** the parity check is complete
- **THEN** a parity report and the migration difference register are signed by the accountant (FR-MIG-02)
- **AND** no difference is left unexplained

### Requirement: Parallel run (M5)
The migration SHALL run in parallel for one month, recording in both the workbook and Fyniq and reconciling at month end.

Source: PRD-001 §17.1

#### Scenario: Parallel month signed off
- **WHEN** the parallel month ends
- **THEN** the workbook and Fyniq are reconciled
- **AND** the accountant signs off

### Requirement: Go-live and workbook archive (M6)
At go-live the workbook SHALL be locked and kept as a read-only archive, with its PDFs, for the retention period.

Source: PRD-001 §17.1

#### Scenario: Archive stored with a hash
- **WHEN** Fyniq goes live
- **THEN** the workbook is locked
- **AND** its read-only archive, with its PDFs, is stored with a hash for the retention period
