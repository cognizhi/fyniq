# reporting/reports Specification

## Purpose
How Fyniq produces financial statements, management and accountant reports, the board pack and scheduled reports, and exports them safely, so that every report can be reproduced exactly as at the ledger revision it read. Derived from PRD-001 §8.12.

## Requirements

### Requirement: FR-RPT-01 Financial statements
The system SHALL produce the financial statements: the P&L, the balance sheet, the cash flow statement and the trial balance.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: Profit and loss
- **WHEN** the P&L is produced
- **THEN** it shows revenue by stream, gross margin, operating expenses by group and function, EBITDA and net profit, each with its % of revenue

#### Scenario: Balance sheet
- **WHEN** the balance sheet is produced
- **THEN** it includes a balance check and the movement in equity

#### Scenario: Cash flow statement
- **WHEN** the cash flow statement is produced
- **THEN** it uses the indirect method and ties to the bank balances
- **AND** the direct method is available from P2

#### Scenario: Trial balance
- **WHEN** the financial statements are listed
- **THEN** the trial balance is one of them, for any period

#### Scenario: The layout from the chart's attributes (rev 0.159)
- **WHEN** the statements are produced
- **THEN** the P&L runs from revenue by report group through cost of sales (function COGS), gross profit, operating expenses without depreciation and amortisation, EBITDA, depreciation and amortisation, operating profit, other income and finance costs, profit before tax and income tax to net profit
- **AND** the balance sheet groups current and non-current items by report group with borrowings apart, and the indirect cash flow ties to the money accounts with non-cash transactions listed apart [accountant to confirm]

### Requirement: FR-RPT-02 Periods, comparatives and columns
Reports SHALL run for any period, with comparatives and with columns by month or by dimension.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: Any period
- **WHEN** a report is run
- **THEN** the period can be a month, a quarter, YTD, the FY or a custom range

#### Scenario: Comparatives
- **WHEN** comparatives are chosen
- **THEN** the report shows the prior period, the prior year, the budget or the forecast next to the actuals

#### Scenario: Columns by month or dimension
- **WHEN** the column layout is chosen
- **THEN** the report shows columns by month or by dimension

### Requirement: FR-RPT-03 Management reports from the workbook
The system SHALL provide the management reports of the workbook.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: Workbook management reports
- **WHEN** the management reports are listed
- **THEN** they include Budget vs Actual, AR and AP ageing, the customer statement, sales by customer and item, spend by supplier, top expenses, projects, the GST return, KPIs, and cash and runway

### Requirement: FR-RPT-04 Report builder
The system SHALL provide a report builder in which a user chooses rows, columns, filters and calculated rows, and saves, shares and schedules the result.

Source: PRD-001 §8.12 · Priority S · Phase P2

#### Scenario: Building a report
- **WHEN** a user builds a report
- **THEN** rows can be accounts, groups or dimensions, columns can be periods, comparatives or dimensions, and filters and calculated rows can be added

#### Scenario: Save, share and schedule
- **WHEN** a built report is saved
- **THEN** it can be shared and scheduled

### Requirement: FR-RPT-05 Exports
The system SHALL export reports as XLSX, PDF and CSV (Google Sheets in P2), and no export SHALL let text become a formula.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: XLSX export
- **WHEN** a report is exported to XLSX
- **THEN** the file is formatted, has live formulas for totals where helpful and a data tab, and respects the precision boundary in ENG-10

#### Scenario: PDF and CSV exports
- **WHEN** a report is exported to PDF or CSV
- **THEN** the PDF is print-quality and branded
- **AND** the CSV is raw and streams for large data

#### Scenario: Google Sheets export
- **GIVEN** phase P2
- **WHEN** a user exports a report
- **THEN** Google Sheets is offered as a destination

#### Scenario: Amounts are written as typed numbers
- **WHEN** amounts and quantities are exported
- **THEN** they are written as typed numbers: number cells in XLSX, and in CSV only a value that matches the strict decimal pattern of FR-UX-20

#### Scenario: Text cells are neutralised
- **WHEN** a text cell is exported
- **THEN** it is wrapped in double quotes with inner quotes doubled
- **AND** it is also prefixed with an apostrophe when it starts with =, +, -, @, tab, CR or LF (OWASP CSV injection guidance)

#### Scenario: Only the platform writes formulas
- **WHEN** an export contains a live formula
- **THEN** the formula was written by the platform, never taken from user text

#### Scenario: G4-098 Formula-like text and a negative amount
- **GIVEN** a contact named `=HYPERLINK("…")`, a note `+1-2` and an amount of −1,234.56
- **WHEN** they are exported to CSV and XLSX
- **THEN** the name and the note are neutralised text
- **AND** the amount is a number with an ASCII minus

### Requirement: FR-RPT-06 Board pack
The system SHALL build a templated monthly board pack (cover, highlights, P&L, BS, CF, KPIs, runway, commentary) from saved reports.

Source: PRD-001 §8.12 · Priority S · Phase P2

#### Scenario: Built from saved reports
- **WHEN** a month's board pack is built
- **THEN** its sections come from saved reports

#### Scenario: Commentary drafted by AI, edited by a human
- **WHEN** the commentary is prepared
- **THEN** the AI drafts it and a human edits it

#### Scenario: PDF and access-controlled web link
- **WHEN** the board pack is published
- **THEN** it is available as a PDF and as a web link with access control

### Requirement: FR-RPT-07 Scheduled reports
The system SHALL email reports or post them to Slack on a schedule, checking permissions at send time.

Source: PRD-001 §8.12 · Priority S · Phase P2

#### Scenario: Report sent on schedule
- **GIVEN** a report scheduled for email or Slack
- **WHEN** its scheduled time arrives
- **THEN** the report is emailed or posted to Slack

#### Scenario: Permissions checked at send time
- **WHEN** a scheduled report is sent
- **THEN** each recipient's permissions are checked at that time

### Requirement: FR-RPT-08 Reproducible reports
Each generated report SHALL record the ledger version (high-water mark) and parameters it used, so that re-running it "as at" that version returns identical figures.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: What a generated report records
- **WHEN** a report is generated
- **THEN** it reads its figures in one snapshot
- **AND** it records the ledger revision visible in that snapshot (ENG-46), its parameters and filters, the platform version (REL-05) and, where used, the template and parameter-pack versions

#### Scenario: Re-run as at the recorded revision
- **WHEN** a report is re-run "as at" its recorded revision
- **THEN** it returns identical figures, with the account names, groups and dimension values in force at that revision

#### Scenario: Restated presentation
- **WHEN** a report is re-run with today's presentation
- **THEN** it is labelled "restated presentation"

#### Scenario: Identical bytes come from the stored file
- **WHEN** identical bytes of an issued report are needed
- **THEN** they come from the stored issued file (FR-DOC-04), not from a re-run

#### Scenario: Re-runs before the chart keeps its history (rev 0.159)
- **WHEN** a report is re-run as at a revision before the chart keeps its history
- **THEN** its figures are that revision's exactly, shown with today's account names and groups
- **AND** it is labelled "restated presentation" when the chart changed after the revision, and an issued pack's original names come from its stored file (FR-DOC-04)

#### Scenario: G4-051 Back-dated posting during report generation
- **GIVEN** a back-dated posting transaction starts before a P&L is generated and commits after it
- **WHEN** the P&L is re-run "as at" its recorded revision a month later
- **THEN** the figures are identical
- **AND** the late entry has a higher revision and appears only in a new run

#### Scenario: G4-052 Account renamed and regrouped after a board pack
- **GIVEN** a board pack was issued, and an account was then renamed and moved to another report group
- **WHEN** the pack is re-run "as at" its revision and with "restated presentation"
- **THEN** the re-run "as at" the pack's revision shows the original names and grouping
- **AND** "restated presentation" shows the new ones, labelled
- **AND** the stored PDF still matches its hash (FR-DOC-04)
- **AND** the cache does not serve the old presentation to a new run

### Requirement: FR-RPT-09 Asynchronous exports and their audit
Large exports SHALL run asynchronously and deliver a short-lived signed link, and every export SHALL be logged in the audit trail (who, what, filters, row count).

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: Large export delivered by link
- **WHEN** a large export is requested
- **THEN** it runs asynchronously and delivers a short-lived signed link

#### Scenario: Link validity and sign-in
- **WHEN** the link is used
- **THEN** it works for 24 hours (setting) and only after sign-in as the requesting user

#### Scenario: Permissions re-checked at each download
- **WHEN** the link is used to download
- **THEN** the API re-checks the user's permissions
- **AND** it redirects to a storage URL valid for at most 60 seconds

#### Scenario: Revocation stops downloads at once
- **GIVEN** the requesting user's role changed or their access was revoked after the export
- **WHEN** the link is used
- **THEN** the download is refused

#### Scenario: Every export is audited
- **WHEN** any export is made
- **THEN** the audit trail records who exported, what, the filters and the row count

#### Scenario: G4-065 Link used after role removal or without sign-in
- **GIVEN** a user's export link
- **WHEN** it is used after their role was removed, and by someone not signed in
- **THEN** both are refused

### Requirement: FR-RPT-10 Accountant exports
The system SHALL provide exports for the accountant: GL, TB, journals, fixed asset schedule, AR/AP detail and GST detail, in formats for common audit tools.

Source: PRD-001 §8.12 · Priority M · Phase P1

#### Scenario: Accountant export set
- **WHEN** the accountant exports for a period
- **THEN** the GL, TB, journals, fixed asset schedule, AR/AP detail and GST detail are available in formats for common audit tools

#### Scenario: Auditor evidence pack
- **GIVEN** phase P2
- **WHEN** the accountant prepares for an audit
- **THEN** an auditor evidence pack can be exported

### Requirement: FR-RPT-11 Statements in the recorded framework
The system SHALL produce financial statements in the framework and edition recorded in FR-ORG-10, with notes scaffolding.

Source: PRD-001 §8.12 · Priority C · Phase P3

#### Scenario: Framework statements with notes scaffolding
- **GIVEN** a framework and edition recorded for the financial year (FR-ORG-10)
- **WHEN** the financial statements are produced
- **THEN** they follow that framework and edition
- **AND** they include a scaffold for the notes

### Requirement: FR-RPT-12 Consolidated reporting
The system SHALL report consolidated figures across entities with eliminations and currency translation.

Source: PRD-001 §8.12 · Priority C · Phase P3

#### Scenario: Consolidation across entities
- **WHEN** a consolidated report is produced for several entities
- **THEN** the consolidated figures apply eliminations between the entities and currency translation

### Requirement: FR-RPT-13 Deferral roll-forward
The system SHALL show a roll-forward of deferred revenue by customer and contract and of prepayments by supplier, tying to 2400 and 1210 (INV-07).

Source: PRD-001 §8.12 · Priority S · Phase P2

#### Scenario: Roll-forward columns
- **WHEN** the deferral roll-forward is produced
- **THEN** it shows the opening balance, amounts added (invoices, bills), released to the P&L, reversed (credit notes, cancellations) and the closing balance

#### Scenario: Ties to the ledger with drill-down
- **WHEN** the roll-forward is produced
- **THEN** deferred revenue ties to 2400 and prepayments tie to 1210 (INV-07)
- **AND** each line drills down to its schedule

#### Scenario: Billed, collected and recognised revenue
- **WHEN** deferred revenue is viewed per customer
- **THEN** billed, collected and recognised revenue are shown side by side
