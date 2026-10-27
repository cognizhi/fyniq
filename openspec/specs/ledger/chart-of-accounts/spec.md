# ledger/chart-of-accounts Specification

## Purpose
How Fyniq structures the ledger: the pre-loaded Singapore start-up chart of accounts, account attributes and flags, protected system accounts, deactivation and merging, the reporting hierarchy, the dimensions every ledger line carries, and allocation rules. Derived from PRD-001 §8.2.

## Requirements

### Requirement: FR-COA-01 Pre-loaded chart of accounts
The system SHALL pre-load a Singapore start-up chart of accounts (the workbook's 83 accounts and ranges, Appendix B of the workbook PRD), and the chart SHALL be editable.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: An organisation loads the standard chart
- **WHEN** an organisation that has no account yet loads the standard chart
- **THEN** its chart of accounts holds the standard chart's 87 accounts: the workbook's 83 with every attribute, its disposal account moved to 7050, and 2120, 3300 and 7040 (rev 0.31, provisional until the CoA review), and (rev 0.90) 2330 Withholding tax payable [accountant to confirm the code], and (rev 0.151) 1160 Asset disposal clearing, 88 in all [accountant to confirm the code]

#### Scenario: A chart loaded before 1160 (rev 0.151)
- **GIVEN** an organisation whose chart was loaded before 1160 Asset disposal clearing joined the standard chart
- **WHEN** it sells an asset on a sales invoice
- **THEN** it adds 1160 to its chart, or names another account as the disposal clearing account (FR-FA-03)

#### Scenario: The standard chart loads once
- **GIVEN** an organisation that has any account
- **WHEN** the standard chart is loaded
- **THEN** it is refused, and the chart is unchanged

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
- **AND** in the standard chart those are 5050, 6011, 6012, 6013, 6050, 6020 and 6030 (rev 0.31) [verify]

#### Scenario: Payroll-cost flag is protected
- **WHEN** a user changes an account's payroll-cost flag
- **THEN** the change is protected like a change to the control-account flag

#### Scenario: Revalue flag (rev 0.72)
- **WHEN** an account is marked revalue
- **THEN** its foreign-currency balances are revalued at month end (FR-GL-04)
- **AND** only an asset or a liability is marked, never prepayments or deferred revenue (COA-10); the standard chart marks the money accounts, 1100, 2010, 1250, 2500 and 2120

#### Scenario: Default useful life (rev 0.151)
- **WHEN** an asset account of the register (report group Property, plant & equipment or Intangible assets) has a default useful life, in whole months from 1 to 600
- **THEN** the register's form offers it for an asset on that account (FR-FA-01), and the person may change it
- **AND** the standard chart sets 1510 36, 1520 60, 1530 36 and 1540 36 months [verify: common practice; the accountant confirms]
- **AND** changing it is an ordinary chart change, audited, that never changes a registered asset
- **AND** an account outside the register's groups has none

### Requirement: FR-COA-03 Protected system accounts
**System accounts** SHALL be protected: they cannot be deleted, retyped or, where the engine owns them, posted to manually.

Source: PRD-001 §8.2 · Priority M · Phase P1

#### Scenario: Which accounts are system accounts
- **WHEN** the chart is set up
- **THEN** the system accounts are AR, AP, GST control, FX and rounding, deferred revenue, prepayments, salaries payable, CPF and SDL payable, employer CPF, SDL, depreciation, accumulated depreciation, suspense, retained earnings, expense claims payable, director accounts 2500 and 2120, share capital 3010/3020 and capital contribution 3300

#### Scenario: System account cannot be deleted or retyped
- **WHEN** a user tries to delete a system account or change its type
- **THEN** the change is refused

#### Scenario: System accounts carry their roles
- **WHEN** a system account is created
- **THEN** it carries its role, such as accounts-receivable, which never changes
- **AND** each role is on one account, share capital on two (3010, 3020) (rev 0.32)

#### Scenario: A system account cannot be deactivated
- **WHEN** a user tries to deactivate a system account
- **THEN** the change is refused, because the engine posts to it (rev 0.32)

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
