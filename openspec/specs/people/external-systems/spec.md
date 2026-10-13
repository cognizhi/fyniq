# people/external-systems Specification

## Purpose
Using another HR or payroll system instead of Nemura: Fyniq without Nemura, connectors and file import, imported journals as drafts, and mixed use. Derived from PRD-002 §17.

## Requirements

### Requirement: NM-EXT-01 Fyniq without Nemura
A company that uses another HR or payroll system SHALL keep it, Fyniq SHALL record which system holds payroll, leave and people records, and Nemura SHALL NOT be loaded for those features.

Source: PRD-002 §17 · Priority M · Phase P1

#### Scenario: External payroll recorded
- **WHEN** the Owner chooses an external payroll system in Fyniq's People and HR settings
- **THEN** Fyniq records it as the holder of payroll, and Nemura's payroll is not loaded

### Requirement: NM-EXT-02 Connectors
Connectors SHALL bring payroll journals, salary payment lines, the employee projection and leave taken into Fyniq: a file import and one verified Singapore provider in P1, a unified HR API adapter in P2.

Source: PRD-002 §17 · Priority M · Phase P1 (file, one connector) / P2 (unified API)

#### Scenario: File import
- **WHEN** a payroll journal export is imported with a column mapping
- **THEN** Fyniq creates the period's journal from it

### Requirement: NM-EXT-03 Imported journals are drafts
Imported journals SHALL be drafts for a Controller or Accountant to approve, idempotent per period and version, with a preview of changes on re-import.

Source: PRD-002 §17 · Priority M · Phase P1

#### Scenario: Draft
- **WHEN** a September payroll journal is imported
- **THEN** it waits as a draft for a Controller or Accountant

#### Scenario: Re-import
- **WHEN** September is imported again with changes
- **THEN** a preview shows what changes before anything is replaced

### Requirement: NM-EXT-04 Mixed use
A company SHALL be able to use Nemura for some features and an external system for payroll, with Nemura reading pay it needs for letters only from its own record.

Source: PRD-002 §17 · Priority S · Phase P2

#### Scenario: Letters with external payroll
- **GIVEN** payroll lives in an external system and letters in Nemura
- **WHEN** a salary certificate is issued
- **THEN** the salary comes from Nemura's own record, entered or imported
