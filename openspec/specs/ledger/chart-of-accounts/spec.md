# ledger/chart-of-accounts Specification

## Purpose
How Fyniq structures the ledger: the pre-loaded Singapore start-up chart of accounts, account attributes and flags, protected system accounts, deactivation and merging, the reporting hierarchy, the dimensions every ledger line carries, and allocation rules. Derived from PRD-001 §8.2.

## Requirements

### Requirement: FR-COA-01 Pre-loaded chart of accounts
The system SHALL pre-load a Singapore start-up chart of accounts (the workbook's 83 accounts and ranges, Appendix B of the workbook PRD), and the chart SHALL be editable.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: New organisation gets the start-up chart
- **WHEN** a new organisation is created
- **THEN** its chart of accounts holds the workbook's 83 accounts and ranges

#### Scenario: The chart is editable
- **WHEN** a user adds, renames or changes an account in the pre-loaded chart
- **THEN** the change is saved, subject to the protections of FR-COA-03 and FR-COA-04

### Requirement: FR-COA-02 Account attributes
Each account SHALL have a code, name, type (asset, liability, equity, revenue, expense), report group, cash-flow line, function (COGS, R&D, S&M, G&A), money-account flag, default GST code, currency (for bank accounts), control-account flag, payroll-cost flag, active flag and usage guidance.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Usage guidance shown at entry
- **WHEN** a user picks an account while entering a transaction
- **THEN** the account's usage guidance is shown

#### Scenario: Currency for bank accounts
- **WHEN** a bank account is set up in the chart
- **THEN** it records its currency

#### Scenario: Payroll-cost flag
- **WHEN** the chart is set up
- **THEN** the payroll-cost flag is set on the department salary accounts, 6020, 6030 and 6050
- **AND** it is used by FR-PRJ-12 and FR-PAY-13

#### Scenario: Payroll-cost flag is protected
- **WHEN** a user changes an account's payroll-cost flag
- **THEN** the change is protected like a change to the control-account flag

### Requirement: FR-COA-03 Protected system accounts
**System accounts** SHALL be protected: they cannot be deleted, retyped or, where the engine owns them, posted to manually.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Which accounts are system accounts
- **WHEN** the chart is set up
- **THEN** the system accounts are AR, AP, GST control, FX and rounding, deferred revenue, prepayments, salaries payable, CPF and SDL payable, employer CPF, SDL, depreciation, accumulated depreciation, suspense, retained earnings, expense claims payable, director accounts 2500 and 2120, share capital 3010/3020 and capital contribution 3300

#### Scenario: System account cannot be deleted or retyped
- **WHEN** a user tries to delete a system account or change its type
- **THEN** the change is refused

#### Scenario: No manual posting where the engine owns the account
- **GIVEN** a system account the engine owns
- **WHEN** a user tries to post to it manually
- **THEN** the posting is refused

### Requirement: FR-COA-04 Accounts with postings
An account with postings SHALL NOT be deleted; it SHALL be possible to deactivate it (blocked for new postings) or merge it by an approved journal-preserving merge.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Account with postings cannot be deleted
- **GIVEN** an account with postings
- **WHEN** a user tries to delete it
- **THEN** the deletion is refused

#### Scenario: Deactivated account blocks new postings
- **GIVEN** an account with postings is deactivated
- **WHEN** a new posting is made to it
- **THEN** the posting is blocked

#### Scenario: Approved journal-preserving merge
- **WHEN** an account with postings is merged into another account
- **THEN** the merge needs approval
- **AND** the existing journals are preserved

### Requirement: FR-COA-05 Account hierarchy
The system SHALL support a parent/child hierarchy of accounts for reporting roll-ups, with unlimited depth, and posting only to leaves.

Source: PRD-001 §8.2 · Priority S · Phase P1

#### Scenario: Reports roll up the hierarchy
- **GIVEN** accounts nested to any depth
- **WHEN** a report is run
- **THEN** each parent shows the roll-up of its children

#### Scenario: Posting only to leaves
- **WHEN** a user tries to post to a parent account
- **THEN** the posting is refused

### Requirement: FR-DIM-01 Dimensions
The system SHALL provide **dimensions** (tracking): Department, Project, and up to 8 custom dimensions, with values that can be required per account.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Custom dimensions
- **WHEN** an organisation adds custom dimensions (e.g. Product line, Region, Customer segment, Funding source)
- **THEN** up to 8 are allowed in addition to Department and Project

#### Scenario: Dimension required per account
- **GIVEN** an account that requires a value for a dimension
- **WHEN** a line on that account has no value for it
- **THEN** the line cannot be posted

### Requirement: FR-DIM-02 Dimensions on every line and report
Every ledger line SHALL carry its dimension values, and every report SHALL be filterable, groupable and pivotable by dimension.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Ledger lines carry dimension values
- **WHEN** a line is posted
- **THEN** it carries its dimension values

#### Scenario: Reports by dimension
- **WHEN** a user runs any report
- **THEN** it can be filtered, grouped and pivoted by dimension (for example a department P&L or a project P&L)

### Requirement: FR-DIM-03 Allocation rules
The system SHALL post allocation rules (e.g. split shared costs by headcount) as approved journals, and SHALL accept an allocation key from an imported cost or usage file per customer and product.

Source: PRD-001 §8.2 · Priority S · Phase P2

#### Scenario: Allocation posts as an approved journal
- **WHEN** an allocation rule runs
- **THEN** the allocation posts as a journal only when an approver chooses to post it

#### Scenario: Allocation key from a cost or usage file
- **WHEN** a cost or usage file per customer and product is imported as an allocation key (for example a cloud or AI-model provider's export, with the FinOps FOCUS columns as the reference format)
- **THEN** the file's billed total must equal the supplier's bill it explains, or the import is refused

#### Scenario: Unallocated line
- **WHEN** the key cannot place part of a cost
- **THEN** that part stays on an **unallocated** line

#### Scenario: Contribution margin report
- **WHEN** the **contribution margin** report by customer and product is run
- **THEN** it shows revenue less allocated model, cloud and payment-provider costs, as a management view like FR-PRJ-12

#### Scenario: Usage estimates never post
- **WHEN** usage estimates are used in the contribution margin report
- **THEN** they never post (open question 42)
