# ledger/validation Specification

## Purpose
How Fyniq decides whether a record can post: the workbook's status texts ported as validation rules, the meaning of each severity, one validation layer shared by every channel, and messages that say what to fix. Derived from PRD-001 §10.5.

## Requirements

### Requirement: ENG-20 Workbook statuses ported as validation rules
The system SHALL port every workbook status text (more than 100, in priority order per table) as a validation rule with the same plain-English message, adapted where the platform removes the cause.

Source: PRD-001 §10.5 · Priority M · Phase P1

#### Scenario: Workbook status keeps its message
- **GIVEN** a workbook status text whose cause can still occur on the platform
- **WHEN** a record meets that status's condition
- **THEN** the validation rule ported from it shows the same plain-English message

#### Scenario: Priority order per table is kept
- **GIVEN** a record that meets the conditions of more than one ported status of its table
- **WHEN** it is validated
- **THEN** the statuses follow the workbook's priority order for that table

#### Scenario: Status whose cause the platform removes
- **GIVEN** the workbook status "Category not on Accounts sheet"
- **WHEN** a category is chosen on the platform
- **THEN** it is picked from a list, so the status cannot occur
- **AND** the ported rule is adapted to that

### Requirement: ENG-21 Severity semantics
The system SHALL carry over the workbook's severity semantics: ✖ the record cannot post, ⚠ it posts but needs review, ⏰ due or overdue, ✎ draft.

Source: PRD-001 §10.5 · Priority M · Phase P1

#### Scenario: Error means the record cannot post
- **WHEN** a record has a problem of severity ✖
- **THEN** the record cannot post
- **AND** it is saved as a draft with the problem shown

#### Scenario: Warning posts but needs review
- **WHEN** a record has a problem of severity ⚠
- **THEN** the record posts
- **AND** it appears in review queues and on Checks until the problem is acknowledged or fixed

#### Scenario: Due or overdue
- **WHEN** a record is due or overdue
- **THEN** it carries the ⏰ status

#### Scenario: Draft
- **WHEN** a record is a draft
- **THEN** it carries the ✎ status

### Requirement: ENG-22 Validation in the domain layer
Validation SHALL run in the domain layer, shared by the UI, the API, imports and the AI proposal dry run, and the database SHALL enforce the invariants in §10.7 independently.

Source: PRD-001 §10.5 · Priority M · Phase P0

#### Scenario: One set of rules for every channel
- **WHEN** the same record is submitted through the UI, the API, an import or an AI proposal dry run
- **THEN** the same domain-layer validation runs
- **AND** it gives the same result

#### Scenario: The database enforces the invariants independently
- **GIVEN** a write that breaks an invariant of §10.7
- **WHEN** it reaches the database without passing the domain-layer validation
- **THEN** the database still rejects it

### Requirement: ENG-23 Plain-language validation messages
Every validation message SHALL name the field, say what is wrong and how to fix it, and have an error code for tests and support.

Source: PRD-001 §10.5 · Priority M · Phase P1

#### Scenario: Message content
- **WHEN** a validation rule fails
- **THEN** its message names the field
- **AND** it says what is wrong and how to fix it

#### Scenario: Error code
- **WHEN** a validation message is shown or returned
- **THEN** it carries an error code that tests and support can use
