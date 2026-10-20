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

#### Scenario: The Journal sheet's statuses (rev 0.57)
- **GIVEN** a draft journal that does not balance, has no date, or is dated where no period is open
- **WHEN** it is read or listed
- **THEN** it carries the workbook's statuses in its priority order and words, such as "Enter the date" and "Entry does not balance", each with its code, field and fix
- **AND** "Dated before books start" is an error here, since no period exists before the books start

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

#### Scenario: A journal's severities (rev 0.57)
- **WHEN** a journal is read or listed
- **THEN** a draft carries every error (✖) the posting rules find and whether its month is open, or OK (✔)
- **AND** a posted journal carries OK, or "Locked (closed month)" once its month is closed

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

### Requirement: ENG-24 Manual journal validity
A manual journal SHALL post only when every validity rule holds; otherwise it SHALL be rejected whole, post no line and report every failure at once with its field and code. The engine SHALL never add, drop or change a line to make a journal post (PRD-001 §10.2: as entered).

Source: PRD-001 §10.5 · Priority M · Phase P0

#### Scenario: Rejected whole, every failure at once
- **WHEN** a manual journal breaks one or more rules
- **THEN** it is rejected whole (✖, ENG-21) and posts no line
- **AND** every failure is reported at once with its field and code (ENG-23, FR-INT-07)

#### Scenario: Rule 1, currencies and the input boundary
- **WHEN** a line's currency is not one the organisation uses, or an amount or rate fails the input boundary (ENG-44)
- **THEN** the journal is rejected

#### Scenario: Rule 2, at least two lines
- **WHEN** a manual journal has fewer than two lines
- **THEN** it is rejected

#### Scenario: Rule 3, no zero line
- **WHEN** a line's amount is zero
- **THEN** the journal is rejected
- **AND** a non-zero line whose base amount rounds to zero still posts, with base amount 0.00 (IDR 50 at SGD 0.000085 per IDR; INV-05)

#### Scenario: Rule 4, postable accounts
- **WHEN** a line's account does not exist, is inactive or is a group account
- **THEN** the journal is rejected (INV-04)

#### Scenario: Rule 5, rates and their sources
- **WHEN** a line not in the base currency has no rate or no source, or a rate is entered on a base-currency line
- **THEN** the journal is rejected (ENG-03)

#### Scenario: Rule 6, base amounts fit their column
- **WHEN** a line's computed base amount does not fit its column
- **THEN** the journal is rejected (ENG-44)

#### Scenario: Rule 7, balanced in base
- **WHEN** the base amounts, each `RND-FX` of its line, do not sum to zero
- **THEN** the journal is rejected (INV-01)

#### Scenario: Rule 8, a one-currency journal balances in its currency
- **GIVEN** every line of a journal is in one currency
- **WHEN** the amounts do not sum to zero in that currency
- **THEN** the journal is rejected (INV-01, FR-GL-01)

#### Scenario: The engine never repairs a journal
- **GIVEN** a journal in USD, 0.01 + 0.01 − 0.02 at 1.5, whose base amounts are SGD 0.02 + 0.02 − 0.03
- **WHEN** it is posted
- **THEN** it is rejected as unbalanced in base, naming the difference
- **AND** the engine neither adds a 7030 line nor changes a base amount; the preparer adds a base-currency line to 7030 FX and rounding (FR-GL-01)

#### Scenario: The invariants apply as well
- **WHEN** a manual journal line posts to a share-capital account
- **THEN** it is refused (INV-13)
