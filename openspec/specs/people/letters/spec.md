# people/letters Specification

## Purpose
Letters and templates in Nemura: letter types, templates on Fyniq's engine, two-step issue with a passkey signature, automatic standard letters, verification and KETs. Derived from PRD-002 §11.

## Requirements

### Requirement: NM-LTR-01 Letter types
Each letter type SHALL set who may request it, which fields it shows, who signs it and how fast it is due, with salary shown only when the person asks.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Salary only on request
- **WHEN** an employee requests an employment letter for a bank and asks to include salary
- **THEN** the letter shows the salary; otherwise it does not

### Requirement: NM-LTR-02 Templates
Letter templates SHALL use Fyniq's template engine and editor with people merge fields, and a template change SHALL be versioned and approved by the HR administrator.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Merge fields
- **WHEN** a template uses the person's start date and the company's UEN
- **THEN** the issued letter fills them from the record

#### Scenario: Approved change
- **WHEN** a template is edited
- **THEN** the new version applies only after the HR administrator approves it

### Requirement: NM-LTR-03 Issue in two steps
A letter SHALL be filled from the request, checked by an HR officer, signed by the authorised signatory with a passkey, delivered in My requests and by an expiring email link, and kept as evidence.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Issue
- **WHEN** an HR officer checks a filled letter and the signatory signs it with a passkey
- **THEN** the employee gets it in My requests and by an expiring link, and it is kept as evidence

### Requirement: NM-LTR-04 Standard letters without waiting
Letter types marked "issue automatically" SHALL be generated and signed with the pre-approved signature as soon as they are requested, and logged.

Source: PRD-002 §11 · Priority S · Phase P1

#### Scenario: Automatic
- **GIVEN** employment confirmation without salary is marked "issue automatically"
- **WHEN** an employee requests it
- **THEN** it is ready at once and the issue is logged

### Requirement: NM-LTR-05 Verification
Every issued letter SHALL carry a reference and a verification link that shows only its own facts, which the employee can revoke.

Source: PRD-002 §11 · Priority S · Phase P2

#### Scenario: Verify
- **WHEN** a bank opens the verification link
- **THEN** it sees the letter's own facts and that it is genuine, and nothing else

### Requirement: NM-LTR-06 KETs
KETs SHALL be generated from the offer and the record and issued within 14 days of the start for employment of 14 days or more, and missing KETs SHALL be ✖ on Checks.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Missing KETs
- **WHEN** a person started 14 days ago and has no issued KETs
- **THEN** Checks shows ✖ (N.1 #1)
