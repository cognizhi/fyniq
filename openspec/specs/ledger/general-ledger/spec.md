# ledger/general-ledger Specification

## Purpose
How Fyniq keeps the general ledger: manual, recurring and auto-reversing journals, schedules, FX revaluation and rates, opening balances, the period close and its locks, year-end, the general ledger report, versioned corrections by reversal, intercompany journals and flux analysis. Derived from PRD-001 §8.8.

## Requirements

### Requirement: FR-GL-01 Manual journals
The system SHALL support manual journals with any number of lines, dimensions and attachments, balanced in base currency (and per currency for foreign-currency journals), with the preparer and approver recorded.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Any number of lines with dimensions and attachments
- **WHEN** a manual journal is entered
- **THEN** it can have any number of lines, each with its dimensions, and attachments

#### Scenario: Unbalanced journal is refused
- **WHEN** a manual journal does not balance in base currency
- **THEN** it cannot be posted

#### Scenario: Foreign-currency journal balances per currency
- **GIVEN** a foreign-currency journal
- **WHEN** it does not balance in each of its currencies
- **THEN** it cannot be posted

#### Scenario: Preparer and approver recorded
- **WHEN** a manual journal is posted
- **THEN** its preparer and its approver are recorded on it

### Requirement: FR-GL-02 Recurring and auto-reversing journals
The system SHALL support recurring journals and **auto-reversing** journals.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Recurring journal
- **GIVEN** a recurring journal with a schedule
- **WHEN** each scheduled date arrives
- **THEN** the journal is created for that date

#### Scenario: Auto-reversing accrual
- **GIVEN** an accrual posted as an auto-reversing journal
- **WHEN** the next period begins
- **THEN** the journal reverses on the 1st of the next period

### Requirement: FR-GL-03 Schedules
The system SHALL keep **schedules** for prepayments and deferred revenue (from bills, invoices and transactions with "spread over months"), depreciation and amortisation, recognising up to the current period only (workbook "as of" gate).

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Spread over months creates a schedule
- **WHEN** a bill, invoice or transaction is marked "spread over months"
- **THEN** a prepayment or deferred revenue schedule is created for it

#### Scenario: Depreciation and amortisation schedules
- **WHEN** an asset is depreciated or amortised
- **THEN** its depreciation or amortisation follows a schedule

#### Scenario: Recognition up to the current period only
- **GIVEN** a schedule that runs beyond the current period
- **WHEN** recognition runs
- **THEN** amounts are recognised up to the current period only (workbook "as of" gate)

### Requirement: FR-GL-04 FX revaluation
The system SHALL revalue the open foreign-currency monetary items at month end at the period-end rate, posting the unrealised gain or loss as base-only lines (INV-05) with automatic reversal on day 1 of the next period.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Eligible monetary items
- **WHEN** month-end revaluation runs
- **THEN** it revalues the open **monetary** items: AR and AP documents, unapplied customer and supplier credits, money accounts (bank, card, wallet, provider clearing), and foreign-currency balances on 1250, 2500, 2120, and on 2700 when the accountant classified the instrument as a liability (FR-EQ-18)

#### Scenario: Non-monetary items are not revalued
- **WHEN** month-end revaluation runs
- **THEN** non-monetary items (1210, 2400, customer deposits and supplier advances for goods or services not yet delivered) are not revalued [accountant to confirm per account]

#### Scenario: Unrealised amount per item and currency
- **WHEN** an item is revalued
- **THEN** per item and currency, unrealised = `RND-FX`(open foreign amount × period-end rate) − carrying base
- **AND** the rate comes from the cascade (ENG-40) for the period's last day and is stored with its source

#### Scenario: Reversal on day 1 avoids double counting
- **WHEN** the next period begins
- **THEN** the revaluation entry reverses on day 1
- **AND** a later settlement measures realised FX against the document's stored rate (ENG-14, ENG-41), so nothing is counted twice

#### Scenario: Back-dated posting into a revalued month
- **GIVEN** a month already revalued
- **WHEN** a posting is back-dated into it
- **THEN** that month's revaluation re-runs as a reversal and replacement (FR-GL-12)

#### Scenario: G4-039 USD invoice revalued, part-settled and revalued again
- **GIVEN** a USD invoice of USD 10,000 at 1.35 (S$13,500.00); 31 Mar rate 1.30; 15 Apr USD 4,000 received at 1.32; 30 Apr rate 1.33
- **WHEN** March and April are revalued and the receipt is posted
- **THEN** 31 Mar: Dr 7040 500.00 · Cr 1100 500.00 as base-only lines (USD 0), reversed 1 Apr
- **AND** 15 Apr: Dr Bank USD 4,000 (S$5,280.00), Dr 7030 120.00, Cr 1100 USD 4,000 (S$5,400.00)
- **AND** 30 Apr: USD 6,000 carried at S$8,100.00 revalued to S$7,980.00, Dr 7040 120.00 · Cr 1100 120.00
- **AND** the USD open balance stays 6,000; INV-05 accepts base-only lines only from the revaluation source

### Requirement: FR-GL-05 FX rates
The system SHALL take FX rates from an automatic daily feed (e.g. MAS rates from its API catalogue, redistribution confirmed with MAS) plus manual overrides with audit, and SHALL record the rate source on every converted amount.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Daily rate feed
- **WHEN** the daily rate feed runs
- **THEN** the day's rates are loaded automatically

#### Scenario: Manual override is audited
- **WHEN** a user overrides a rate manually
- **THEN** the override is audited

#### Scenario: Rate source on every converted amount
- **WHEN** an amount is converted
- **THEN** the source of the rate used is recorded with it

### Requirement: FR-GL-06 Opening balances
The system SHALL take opening balances at books start with a balance check, and SHALL import opening open items (invoices, bills) with their currencies.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Opening balances are checked
- **WHEN** opening balances at books start are entered
- **THEN** a balance check runs on them

#### Scenario: Opening open items keep their currencies
- **WHEN** opening invoices and bills are imported
- **THEN** each is imported as an open item with its currency

### Requirement: FR-GL-07 Period close workflow
The system SHALL provide a **period close workflow** per month: a close checklist, checks that must be clean, reconciliations signed off and an attestation (FM-4); closing locks the period.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Close checklist from templates
- **WHEN** a month's close starts
- **THEN** its checklist comes from templates, with tasks that each have an owner, a due date and evidence

#### Scenario: Close needs clean checks, sign-offs and attestation
- **WHEN** the period is closed
- **THEN** the checks must be clean, the reconciliations signed off and the attestation (FM-4) given
- **AND** closing locks the period

#### Scenario: Ledger revision recorded
- **WHEN** a check, a reconciliation or the attestation is completed
- **THEN** it records the ledger revision it saw (ENG-46)

#### Scenario: Close commits only on current checks
- **WHEN** the close commits
- **THEN** no entry dated on or before the period end has a revision later than the lowest revision the checks, reconciliations and attestation recorded
- **AND** the AUD-04 snapshot records the revision at close

#### Scenario: Stale checks fail the close
- **GIVEN** an entry dated on or before the period end has a revision later than the lowest recorded revision
- **WHEN** the close is attempted
- **THEN** the close fails with a stable error and asks for the checks to be re-run

#### Scenario: G4-053 Back-dated posting and close at the same time
- **GIVEN** a back-dated posting to June and the close of June run at the same time on two connections
- **WHEN** both try to commit
- **THEN** either the posting commits first and the close fails "checks are stale, re-run", or the close commits first and the posting is refused with a stable error
- **AND** there is never an entry in a closed June
- **AND** the close snapshot's revision matches its trial balance

### Requirement: FR-GL-08 Period lock
The database SHALL enforce the **period lock**: no posting with a date in a **closed** period (the hard lock), by anyone; the soft locks of FR-GL-09 are weaker states.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Posting into a closed period is refused
- **GIVEN** a closed period
- **WHEN** anyone, by any path, tries to post with a date in it
- **THEN** the database refuses the posting

#### Scenario: Reopening needs dual approval and a reason
- **WHEN** a closed period is reopened
- **THEN** the reopening needs dual approval and a reason

#### Scenario: Reopening is logged with snapshots
- **WHEN** a closed period is reopened
- **THEN** the reopening is logged with before and after snapshots

### Requirement: FR-GL-09 Lock levels and soft locks
The system SHALL provide two lock levels, the hard close of FR-GL-08 for all users and a **soft lock** for all users except the accountant/adviser role (§11.4), plus per-module soft locks and an exceptions report.

Source: PRD-001 §8.8 · Priority S · Phase P2

#### Scenario: Per-module soft locks
- **WHEN** AR is soft-locked for a period while AP stays open
- **THEN** AR postings dated in the period are blocked while AP postings are accepted

#### Scenario: Soft-locked period accepts only the accountant
- **GIVEN** a soft-locked period
- **WHEN** a posting is dated in it
- **THEN** it is accepted only from the accountant/adviser role, with a reason
- **AND** it is listed on the exceptions report

#### Scenario: Where each lock is enforced
- **WHEN** a posting meets a lock
- **THEN** a soft lock is enforced by the domain layer and the hard close by the database (INV-03)

#### Scenario: Exceptions report
- **WHEN** the exceptions report is run
- **THEN** it lists postings dated near or before a lock date

### Requirement: FR-GL-10 Year-end
The system SHALL compute retained earnings at **year-end** without requiring closing entries, offer an optional year-end closing entry for statutory presentation, and carry open items forward automatically (one continuous ledger).

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Retained earnings without closing entries
- **WHEN** a financial year ends
- **THEN** retained earnings are computed with no closing entries required

#### Scenario: Optional closing entry
- **WHEN** a user chooses a year-end closing entry for statutory presentation
- **THEN** the closing entry is posted

#### Scenario: Open items carry forward
- **WHEN** a new financial year begins
- **THEN** open items carry forward automatically in the one continuous ledger

### Requirement: FR-GL-11 General ledger report
The system SHALL provide a general ledger report for any account or accounts and any date range, with running balance, export and drill to source.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Report with running balance
- **WHEN** a user runs the general ledger report for chosen accounts and a date range
- **THEN** it lists the postings with a running balance

#### Scenario: Export and drill to source
- **WHEN** a user exports the report or opens a line
- **THEN** the report is exported, or the line's source document opens

### Requirement: FR-GL-12 Posted documents are never edited in place
Posted documents SHALL never be edited in place: an edit in an open period SHALL create a new version, posted as reversal and replacement entries, and a void SHALL be a reversal with a reason.

Source: PRD-001 §8.8 · Priority M · Phase P1

#### Scenario: Edit creates a new version
- **GIVEN** a posted document in an open period
- **WHEN** it is edited
- **THEN** a new version is created and the engine posts reversal and replacement entries
- **AND** history keeps both versions

#### Scenario: Void is a reversal with a reason
- **WHEN** a posted document is voided
- **THEN** it is reversed, and the reason is recorded

### Requirement: FR-GL-13 Intercompany journals and eliminations
The system SHALL support intercompany journals and eliminations.

Source: PRD-001 §8.8 · Priority C · Phase P3

#### Scenario: Intercompany journal
- **WHEN** a journal between related organisations is entered
- **THEN** it is recorded as an intercompany journal

#### Scenario: Eliminations
- **GIVEN** intercompany journals
- **WHEN** eliminations are run
- **THEN** the intercompany amounts are eliminated

### Requirement: FR-GL-14 Flux analysis
The close SHALL include **flux analysis**: month-on-month and budget variances per account above a threshold need a short explanation before the close is signed off.

Source: PRD-001 §8.8 · Priority S · Phase P1

#### Scenario: Variance above threshold needs an explanation
- **GIVEN** an account's month-on-month or budget variance is above the threshold
- **WHEN** the close is signed off
- **THEN** sign-off needs a short explanation of that variance

#### Scenario: AI may draft the explanation
- **WHEN** a variance needs an explanation
- **THEN** the AI may draft it
