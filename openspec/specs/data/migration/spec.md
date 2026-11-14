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

#### Scenario: Payroll as monthly summary journals (rev 0.254)
- **WHEN** the Payroll sheet is imported
- **THEN** each month's pay run becomes one posted summary journal keeping the amounts actually paid and reported to the CPF Board (gross pay, employee and employer CPF, SDL, SHG, net pay) by salary account and department
- **AND** the per-employee rows stay in the archived workbook until the payroll link

#### Scenario: Only into an organisation with no posted entries (rev 0.254)
- **WHEN** a workbook import is started in an organisation that has posted entries
- **THEN** it is refused, and a failed or rejected import starts again in a new organisation, never mixed into live books

#### Scenario: Projects and departments as dimension values (rev 0.254)
- **WHEN** the workbook's projects and the Employees sheet's departments are imported
- **THEN** each project becomes a value of the project dimension and each department a value of the department dimension, with their tags on transactions, invoices, bills and journals carried over
- **AND** the Timesheets sheet and the per-person employee rows stay in the archived workbook

#### Scenario: One approved commit, run in resumable batches (rev 0.258)
- **WHEN** the import is committed
- **THEN** it is one approval request, prepared under C on "Workbook import (migration)" and approved by an Owner who did not prepare it, over the workbook, its validation report and the signed mapping
- **AND** once approved it runs in batches of at most 50 records, each one request on the API's own database role through the domain commands, each record in its own transaction with its business key, its sheet and row and the approval
- **AND** closing the page pauses it, the next batch resumes at the first record not yet imported, and the approval is its one entry in the weekly review

#### Scenario: Imported documents keep their numbers (rev 0.259)
- **WHEN** the workbook's invoices and journals are imported
- **THEN** each keeps the workbook's number and its sequence continues past the highest number of its own form, so no number repeats
- **AND** a bill keeps the workbook's number as the supplier's reference

#### Scenario: The people behind director accounts (rev 0.259)
- **WHEN** the mapping is proposed
- **THEN** it names the Funding sheet's founders and everyone named on a director's or subscriber's account, each mapped to a contact marked director or shareholder
- **AND** each such account with an opening balance names whose balance it is
- **AND** each supplier resident for tax outside Singapore names the nature of payment its bills record and the tax agent's decision on withholding, which the Accountant enters before signing

#### Scenario: Transactions as statement lines (rev 0.260)
- **WHEN** the Transactions sheet is imported
- **THEN** each row is a line of its money account's statement, and a transfer between two money accounts a line on each side
- **AND** each line becomes the receipt of the invoice it names, the payment of the bill it names, a transfer, or a categorised bank transaction

#### Scenario: The Funding sheet's cap table and SAFEs (rev 0.262)
- **WHEN** the Funding sheet is imported
- **THEN** each holding issued before the books start is in the register of members with no entry, and one a Transactions row pays for later is that row's money to share application monies and a cash issue effective on its day
- **AND** each SAFE or note is a recorded instrument, which its line on 2700 names, a clean start's 2700 balance a line for each
- **AND** the option pool stays in the archived workbook with a warning, and the import's equity records need no evidence attached, the archived workbook being their evidence

#### Scenario: Recurring rows and the budget carry on (rev 0.263)
- **WHEN** either path is imported
- **THEN** each active Recurring invoice, bill or journal row becomes a recurring invoice, bill or journal whose first occurrence is the first after the workbook's as-of
- **AND** a Recurring bank transaction, an inactive row and one Fyniq cannot schedule stay in the archived workbook with a warning
- **AND** the Budget sheet becomes a recorded original budget for each financial year its months reach from the books start

#### Scenario: The forecast from the workbook's drivers (rev 0.264)
- **WHEN** either path is imported
- **THEN** the Forecast sheet's drivers, plans and Base, Best and Worst levers become a recorded forecast with the Settings sheet's tax settings
- **AND** on the history it starts from the workbook's last actual month, so the books give it the cash, receivables and payables the sheet shows
- **AND** on a clean start it reads its books from the last closed month

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

#### Scenario: The parity report (rev 0.265)
- **WHEN** a committed import is reconciled
- **THEN** each account's opening and each month's movement and closing through the workbook's as-of are compared with its Trial Balance sheet after RND-IMPORT, a clean start's openings with the Carry Forward sheet
- **AND** each figure that differs is a register line with a proposed class: a on a spread or depreciation account within a cent a month, b on a revaluation or statutory account, else none for the accountant
- **AND** the workbook's Checks rows are compared by their C.1 number with Fyniq's at their last run, one never run counting as different

#### Scenario: The register decided and signed (rev 0.266)
- **WHEN** the Accountant decides the register's lines and signs the report
- **THEN** each decision is a, b or c on the report as read, a workbook defect with a note, and it goes stale once the line's figures move
- **AND** the Accountant, stepped up, signs the report and the register together over the report's hash once every line has a decision that counts and Fyniq's checks have run
- **AND** a report is signed once, and books or decisions that change afterwards make another report on which the earlier sign-off is not current

#### Scenario: Open items and payroll totals (rev 0.267)
- **WHEN** a committed import is reconciled
- **THEN** each customer's and supplier's open total at the as-of is compared with the workbook's AR & AP sheet, and each month's net pay, CPF Board dues and employer's cost with its Payroll sheet
- **AND** each difference is a register line of its own
- **AND** the report names the GST F5 comparison as not compared until Fyniq makes the F5 return

### Requirement: FR-MIG-03 Generic import
The system SHALL provide a generic CSV/XLSX import for each entity, with templates and a dry run.

Source: PRD-001 §8.19 · Priority M · Phase P1

#### Scenario: Template per entity
- **WHEN** a user prepares a CSV or XLSX import for an entity
- **THEN** a template for that entity is available

#### Scenario: Dry run
- **WHEN** a user runs the import as a dry run
- **THEN** the result is shown without anything being committed

#### Scenario: Each row through its own command (rev 0.270)
- **WHEN** a file filled in an entity's template, or the workbook's own sheet, is imported under that entity's right C
- **THEN** each row becomes its record through the command that enters it by hand, in any organisation
- **AND** a file with a row refused imports nothing and names each problem's row and column as the file numbers them

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

#### Scenario: The parallel month's workbook reconciled (rev 0.268)
- **WHEN** the workbook at the end of the parallel month is uploaded once the parity report is signed
- **THEN** it is read without a mapping and reconciled with Fyniq the same way through its own as-of, its lines in the register M4 began
- **AND** the Accountant, stepped up, signs its report over its hash once every line has a decision that counts

### Requirement: Go-live and workbook archive (M6)
At go-live the workbook SHALL be locked and kept as a read-only archive, with its PDFs, for the retention period.

Source: PRD-001 §17.1

#### Scenario: Archive stored with a hash
- **WHEN** Fyniq goes live
- **THEN** the workbook is locked
- **AND** its read-only archive, with its PDFs, is stored with a hash for the retention period

#### Scenario: Go-live archives and closes the migration (rev 0.269)
- **WHEN** the parallel month is signed and a person with C on the import, stepped up, archives it
- **THEN** each of the migration's files, its workbooks, Checks exports and the PDFs of the workbook's reports, is copied write-once to the retention-locked store with its hash, kept for the records' retention
- **AND** the migration closes, read only, never before every file is archived
