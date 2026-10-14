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

#### Scenario: Evidence kept
- **WHEN** a letter is issued
- **THEN** NM-LTR-07's evidence is kept

### Requirement: NM-LTR-04 Standard letters without waiting
Letter types an authorised signatory delegates for automatic issue SHALL be generated and signed as soon as they are requested, within the delegation's type, template version, fields, requesters, limit and end date, and logged with the exact bytes and the requester.

Source: PRD-002 §11 · Priority S · Phase P1

#### Scenario: Automatic
- **GIVEN** the signatory has delegated automatic issue of employment confirmation without salary
- **WHEN** an employee requests it
- **THEN** it is ready at once and the issue is logged

#### Scenario: Delegation ended
- **GIVEN** the delegation expired yesterday
- **WHEN** an employee requests the letter
- **THEN** it goes through the normal two-step issue (NM-LTR-03)
- **AND** an AI draft never uses the automatic path

#### Scenario: Template changed
- **WHEN** the letter's template changes
- **THEN** the delegation ends

#### Scenario: Not eligible
- **GIVEN** an employee serving notice
- **WHEN** they request an active-employment confirmation
- **THEN** it is not issued automatically

### Requirement: NM-LTR-05 Verification
Every issued letter SHALL carry a reference and code that let a recipient check whether it is valid, replaced or withdrawn without seeing personal facts, which only the employee's expiring, revocable disclosure link SHALL show.

Source: PRD-002 §11 · Priority S · Phase P2

#### Scenario: Verify
- **WHEN** a bank enters the letter's reference and code
- **THEN** it sees that the letter is valid, with its type, date and company, and no salary or other personal fact

#### Scenario: Guessing references
- **WHEN** someone tries references without their codes
- **THEN** no letter is found, and the attempts are rate-limited and audited

#### Scenario: Revoking a share
- **WHEN** the employee revokes a disclosure link
- **THEN** the link stops working, and the letter stays valid

#### Scenario: Replacing a letter
- **WHEN** the employer replaces a wrong letter
- **THEN** both versions and their states stay traceable

### Requirement: NM-LTR-06 KETs
KETs SHALL be generated from the offer and the record and issued within 14 days of the start for employment of 14 days or more, and missing KETs SHALL be ✖ on Checks.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Missing KETs
- **WHEN** a person started 14 days ago and has no issued KETs
- **THEN** Checks shows ✖ (N.1 #1)

#### Scenario: Letters off
- **GIVEN** Letters is off
- **WHEN** a person joins
- **THEN** KETs are still due, through Onboarding or a recorded manual route (NM-FEAT-05)

### Requirement: NM-LTR-07 What an issued letter proves
Each issued letter's exact PDF SHALL be hashed and kept with its template version, data, signatory, authority and signed envelope, SHALL tell recipients how to check it, and a passkey approval SHALL NOT be presented as a certificate signature.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: One byte changed
- **WHEN** one byte of an issued PDF changes
- **THEN** it no longer matches its evidence

#### Scenario: Revoked signatory
- **GIVEN** a signatory's authority was revoked
- **WHEN** they try to sign a new letter
- **THEN** it is refused

#### Scenario: What a recipient can check
- **WHEN** a letter is exported
- **THEN** it states its reference and how to check it, and claims no certificate signature
